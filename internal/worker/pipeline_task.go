package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/davidbyttow/govips/v2/vips"
)

// VariantRepository 变体仓库接口
type VariantRepository interface {
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, fileHash string, width, height int) error
	UpdateFailed(id uint, errMsg string, _ bool) error
	GetByID(id uint) (*models.ImageVariant, error)
	DeleteVariant(id uint) error
}

// ImageRepository 图片仓库接口
type ImageRepository interface {
	UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error
	GetImageByID(id uint) (*models.Image, error)
}

// pipelineResult 处理结果
type pipelineResult struct {
	StoragePath string
	Width       int
	Height      int
	FileSize    int64
	FileHash    string
}

// ImageComplexity 图片复杂度级别
type ImageComplexity int

const (
	ComplexityLow    ImageComplexity = 0 // 低复杂度（截图、纯色）
	ComplexityMedium ImageComplexity = 1 // 中复杂度（普通图）
	ComplexityHigh   ImageComplexity = 2 // 高复杂度（照片）
)

// detectImageComplexity 检测图片复杂度
func detectImageComplexity(img *vips.ImageRef, fileBytes []byte) ImageComplexity {
	hasAlpha := img.HasAlpha()
	if hasAlpha {
		return ComplexityLow
	}

	width := img.Width()
	height := img.Height()
	fileSize := len(fileBytes)
	pixelCount := width * height

	if pixelCount == 0 {
		return ComplexityMedium
	}

	bytesPerPixel := float64(fileSize) / float64(pixelCount)

	// JPEG 图片分析
	switch {
	case bytesPerPixel > 3.0:
		return ComplexityLow
	case bytesPerPixel > 2.0:
		return ComplexityLow
	case bytesPerPixel < 0.8:
		return ComplexityHigh
	case bytesPerPixel < 1.2:
		return ComplexityMedium
	default:
		return ComplexityMedium
	}
}

// adaptiveWebPQuality 根据图片复杂度返回自适应质量
func adaptiveWebPQuality(complexity ImageComplexity, baseQuality int) int {
	switch complexity {
	case ComplexityLow:
		return min(baseQuality-10, 75)
	case ComplexityMedium:
		return baseQuality
	case ComplexityHigh:
		return min(baseQuality+5, 90)
	default:
		return baseQuality
	}
}

// readWithLimit 读取流并检查大小限制
func readWithLimit(r io.Reader, limit int64) ([]byte, error) {
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds max size %d bytes", limit)
	}
	return data, nil
}

// ImagePipelineTask 统一图片处理任务
type ImagePipelineTask struct {
	ThumbVariantID  uint
	WebPVariantID   uint
	ImageID         uint
	StoragePath     string
	ImageIdentifier string
	Storage         storage.Provider
	ConfigManager   *dbconfig.Manager
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	CacheHelper     *cache.Helper
}

// Execute 执行任务
func (t *ImagePipelineTask) Execute() {
	defer t.recovery()

	ctx := context.Background()

	if t.ThumbVariantID > 0 {
		utils.LogIfDevf("[Pipeline] Attempting CAS: thumbnail variant %d pending->processing", t.ThumbVariantID)
		acquired, err := t.VariantRepo.UpdateStatusCAS(
			t.ThumbVariantID,
			models.VariantStatusPending,
			models.VariantStatusProcessing,
			"",
		)
		if err != nil {
			utils.LogIfDevf("[Pipeline] CAS error for thumbnail variant %d: %v", t.ThumbVariantID, err)
			return
		}
		if !acquired {
			utils.LogIfDevf("[Pipeline] CAS failed for thumbnail variant %d (not in pending state)", t.ThumbVariantID)
			return
		}
		utils.LogIfDevf("[Pipeline] CAS success: thumbnail variant %d is now processing", t.ThumbVariantID)
	}

	if t.WebPVariantID > 0 {
		utils.LogIfDevf("[Pipeline] Attempting CAS: WebP variant %d pending->processing", t.WebPVariantID)
		acquired, err := t.VariantRepo.UpdateStatusCAS(
			t.WebPVariantID,
			models.VariantStatusPending,
			models.VariantStatusProcessing,
			"",
		)
		if err != nil {
			utils.LogIfDevf("[Pipeline] CAS error for WebP variant %d: %v", t.WebPVariantID, err)
			return
		}
		if !acquired {
			utils.LogIfDevf("[Pipeline] CAS failed for WebP variant %d (not in pending state)", t.WebPVariantID)
			return
		}
		utils.LogIfDevf("[Pipeline] CAS success: WebP variant %d is now processing", t.WebPVariantID)
	}

	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		utils.LogIfDevf("[Pipeline] Failed to acquire semaphore: %v", err)
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("semaphore: %v", err), true)
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("semaphore: %v", err), true)
		}
		// 更新主图片状态为失败
		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
		t.deleteCacheOnTerminalState("failed")
		return
	}
	defer semaphore.Release()

	utils.LogIfDevf("[Pipeline] Starting processing for image=%s, thumbVariant=%d, webpVariant=%d",
		t.StoragePath, t.ThumbVariantID, t.WebPVariantID)
	if err := t.runPipeline(ctx); err != nil {
		utils.LogIfDevf("[Pipeline] Processing failed: %v", err)
		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
		t.deleteCacheOnTerminalState("failed")
		return
	}

	utils.LogIfDevf("[Pipeline] Processing success for image=%s", t.ImageIdentifier)
}

// runPipeline 执行处理流水线
// 流程：读取文件 -> 顺序处理（先 缩略图 后webp）-> 统一释放
func (t *ImagePipelineTask) runPipeline(ctx context.Context) error {
	maxSize := int64(config.Get().UploadMaxSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024
	}

	stream, err := t.Storage.GetWithContext(ctx, t.StoragePath)
	if err != nil {
		// 获取流失败，标记所有变体为失败
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("get stream: %v", err), true)
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("get stream: %v", err), true)
		}
		return fmt.Errorf("get stream: %w", err)
	}
	if closer, ok := stream.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	fileBytes, err := readWithLimit(stream, maxSize)
	if err != nil {
		// 读取失败，标记所有变体为失败
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("read: %v", err), true)
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("read: %v", err), true)
		}
		return fmt.Errorf("read file: %w", err)
	}

	var thumbResult, webpResult *pipelineResult
	var hasSuccess, hasFailed bool

	// 优先生成缩略图
	if t.ThumbVariantID > 0 {
		result, err := t.generateThumbnail(ctx, fileBytes)
		switch {
		case err != nil:
			utils.LogIfDevf("[Pipeline] Thumbnail failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, err.Error(), true)
			hasFailed = true
		case result == nil:
			utils.LogIfDevf("[Pipeline] Thumbnail skipped, deleting variant %d", t.ThumbVariantID)
			_ = t.VariantRepo.DeleteVariant(t.ThumbVariantID)
		default:
			thumbResult = result
			hasSuccess = true
		}
	}

	// 再生成 WebP 原图
	if t.WebPVariantID > 0 {
		result, err := t.generateWebP(ctx, fileBytes)
		switch {
		case err != nil:
			utils.LogIfDevf("[Pipeline] WebP failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, err.Error(), true)
			hasFailed = true
		case result == nil:
			utils.LogIfDevf("[Pipeline] WebP skipped, deleting variant %d", t.WebPVariantID)
			_ = t.VariantRepo.DeleteVariant(t.WebPVariantID)
		default:
			webpResult = result
			hasSuccess = true
		}
	}

	if hasSuccess {
		t.saveVariantResults(thumbResult, webpResult)
	}

	if hasFailed {
		return fmt.Errorf("some variants failed")
	}

	// 全部成功或全部跳过（如 GIF），都是成功执行
	_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted)
	t.deleteCacheOnTerminalState("success")
	return nil
}

// generateThumbnail 生成缩略图
func (t *ImagePipelineTask) generateThumbnail(ctx context.Context, fileBytes []byte) (*pipelineResult, error) {
	settings, err := t.ConfigManager.GetImageProcessingSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get image processing settings: %w", err)
	}
	if !settings.ThumbnailEnabled {
		utils.LogIfDevf("[Pipeline] Thumbnail generation disabled")
		return nil, nil
	}

	// 跳过 GIF 格式
	if len(fileBytes) > 6 && (string(fileBytes[:6]) == "GIF87a" || string(fileBytes[:6]) == "GIF89a") {
		utils.LogIfDevf("[Pipeline] Skipping GIF thumbnail generation")
		return nil, nil
	}

	if len(settings.ThumbnailSizes) == 0 {
		return nil, fmt.Errorf("no thumbnail sizes configured")
	}
	size := settings.ThumbnailSizes[0]

	utils.LogIfDevf("[Pipeline] Generating thumbnail: width=%d for variant %d", size.Width, t.ThumbVariantID)

	thumbImg, err := vips.NewThumbnailFromBuffer(
		fileBytes,
		size.Width,
		-1,
		vips.InterestingNone,
	)
	if err != nil {
		return nil, fmt.Errorf("thumbnail from buffer: %w", err)
	}
	defer thumbImg.Close()

	width := thumbImg.Width()
	height := thumbImg.Height()

	thumbWebp, _, err := thumbImg.ExportWebp(&vips.WebpExportParams{
		Quality:         settings.ThumbnailQuality,
		Lossless:        false,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export thumbnail webp: %w", err)
	}

	pg := generator.NewPathGenerator()
	thumbIdentifiers := pg.GenerateThumbnailIdentifiers(t.StoragePath, size.Width)
	thumbPath := thumbIdentifiers.StoragePath

	if err := t.Storage.SaveWithContext(ctx, thumbPath, bytes.NewReader(thumbWebp)); err != nil {
		return nil, fmt.Errorf("save thumbnail: %w", err)
	}

	utils.LogIfDevf("[Pipeline] Thumbnail saved: %s (%d bytes)", thumbPath, len(thumbWebp))

	// 计算文件哈希
	fileHash := fmt.Sprintf("%x", sha256.Sum256(thumbWebp))

	return &pipelineResult{
		StoragePath: thumbPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(thumbWebp)),
		FileHash:    fileHash,
	}, nil
}

// generateWebP 生成 WebP 原图
func (t *ImagePipelineTask) generateWebP(ctx context.Context, fileBytes []byte) (*pipelineResult, error) {
	settings, err := t.ConfigManager.GetImageProcessingSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get image processing settings: %w", err)
	}

	return t.generateWebPWithSettings(ctx, fileBytes, settings)
}

// generateWebPWithSettings 使用指定设置生成 WebP
func (t *ImagePipelineTask) generateWebPWithSettings(ctx context.Context, fileBytes []byte, settings *dbconfig.ImageProcessingSettings) (*pipelineResult, error) {
	if !settings.IsFormatEnabled(models.FormatWebP) {
		utils.LogIfDevf("[Pipeline] WebP format disabled")
		return nil, nil
	}

	utils.LogIfDevf("[Pipeline] Generating WebP for variant %d", t.WebPVariantID)

	originImg, err := vips.NewImageFromBuffer(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("load image from buffer: %w", err)
	}
	defer originImg.Close()

	// 检查尺寸限制
	width := originImg.Width()
	height := originImg.Height()
	if settings.MaxDimension > 0 {
		if width > settings.MaxDimension || height > settings.MaxDimension {
			return nil, fmt.Errorf("image exceeds max dimension: %dx%d", width, height)
		}
	}

	// 自适应调整质量
	complexity := detectImageComplexity(originImg, fileBytes)
	adaptiveQuality := adaptiveWebPQuality(complexity, settings.WebPQuality)

	utils.LogIfDevf("[Pipeline] Image complexity=%d, adaptive quality=%d (base=%d)",
		complexity, adaptiveQuality, settings.WebPQuality)

	originWebp, _, err := originImg.ExportWebp(&vips.WebpExportParams{
		Quality:         adaptiveQuality,
		Lossless:        false,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export webp: %w", err)
	}

	pg := generator.NewPathGenerator()
	webpIdentifiers := pg.GenerateConvertedIdentifiers(t.StoragePath, models.FormatWebP)
	originPath := webpIdentifiers.StoragePath

	if err := t.Storage.SaveWithContext(ctx, originPath, bytes.NewReader(originWebp)); err != nil {
		return nil, fmt.Errorf("save webp: %w", err)
	}

	utils.LogIfDevf("[Pipeline] WebP saved: %s (%d bytes, quality=%d)", originPath, len(originWebp), adaptiveQuality)

	// 计算文件哈希
	fileHash := fmt.Sprintf("%x", sha256.Sum256(originWebp))

	return &pipelineResult{
		StoragePath: originPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(originWebp)),
		FileHash:    fileHash,
	}, nil
}

// saveVariantResults 保存变体结果
func (t *ImagePipelineTask) saveVariantResults(thumbResult, webpResult *pipelineResult) {
	// 更新缩略图变体
	if t.ThumbVariantID > 0 && thumbResult != nil {
		_ = t.VariantRepo.UpdateCompleted(
			t.ThumbVariantID,
			filepath.Base(thumbResult.StoragePath),
			thumbResult.StoragePath,
			thumbResult.FileSize,
			thumbResult.FileHash,
			thumbResult.Width,
			thumbResult.Height,
		)
	}

	// 更新 WebP 变体
	if t.WebPVariantID > 0 && webpResult != nil {
		_ = t.VariantRepo.UpdateCompleted(
			t.WebPVariantID,
			filepath.Base(webpResult.StoragePath),
			webpResult.StoragePath,
			webpResult.FileSize,
			webpResult.FileHash,
			webpResult.Width,
			webpResult.Height,
		)
	}
}

// deleteCacheOnTerminalState 终端状态删除缓存
func (t *ImagePipelineTask) deleteCacheOnTerminalState(state string) {
	if t.CacheHelper != nil && t.ImageIdentifier != "" {
		ctx := context.Background()
		if err := t.CacheHelper.DeleteCachedImage(ctx, t.ImageIdentifier); err != nil {
			utils.LogIfDevf("[Pipeline] Failed to delete cache for %s on %s: %v", t.ImageIdentifier, state, err)
		} else {
			utils.LogIfDevf("[Pipeline] Deleted cache for %s after %s", t.ImageIdentifier, state)
		}
	}
}

// recovery 恢复 panic
func (t *ImagePipelineTask) recovery() {
	if rec := recover(); rec != nil {
		utils.LogIfDevf("[Pipeline] Panic recovered: %v", rec)

		if t.ThumbVariantID > 0 {
			_, _ = t.VariantRepo.UpdateStatusCAS(
				t.ThumbVariantID,
				models.VariantStatusProcessing,
				models.VariantStatusFailed,
				fmt.Sprintf("panic: %v", rec),
			)
		}

		if t.WebPVariantID > 0 {
			_, _ = t.VariantRepo.UpdateStatusCAS(
				t.WebPVariantID,
				models.VariantStatusProcessing,
				models.VariantStatusFailed,
				fmt.Sprintf("panic: %v", rec),
			)
		}

		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
	}
}
