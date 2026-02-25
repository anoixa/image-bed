package worker

import (
	"bytes"
	"context"
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
	UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, width, height int) error
	UpdateFailed(id uint, errMsg string, allowRetry bool) error
	GetByID(id uint) (*models.ImageVariant, error)
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
		t.handleFailure(fmt.Errorf("acquire semaphore: %w", err), true, true)
		return
	}
	defer semaphore.Release()

	utils.LogIfDevf("[Pipeline] Starting processing for image=%s, thumbVariant=%d, webpVariant=%d",
		t.StoragePath, t.ThumbVariantID, t.WebPVariantID)
	if err := t.runPipeline(ctx); err != nil {
		utils.LogIfDevf("[Pipeline] Processing failed: %v", err)
		t.handleFailure(err, true, true)
		return
	}

	utils.LogIfDevf("[Pipeline] Processing success for image=%s", t.ImageIdentifier)
}

// runPipeline 执行处理流水线
// 流程：读取文件 -> 顺序处理（先 WebP 后缩略图）-> 统一释放
func (t *ImagePipelineTask) runPipeline(ctx context.Context) error {
	maxSize := int64(config.Get().UploadMaxSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024
	}

	stream, err := t.Storage.GetWithContext(ctx, t.StoragePath)
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}

	fileBytes, err := readWithLimit(stream, maxSize)
	if closer, ok := stream.(io.Closer); ok {
		_ = closer.Close()
	}
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var thumbResult, webpResult *pipelineResult

	if t.WebPVariantID > 0 {
		result, err := t.generateWebP(ctx, fileBytes)
		if err != nil {
			utils.LogIfDevf("[Pipeline] WebP failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, err.Error(), true)
		} else {
			webpResult = result
		}
	}

	if t.ThumbVariantID > 0 {
		result, err := t.generateThumbnail(ctx, fileBytes)
		if err != nil {
			utils.LogIfDevf("[Pipeline] Thumbnail failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, err.Error(), true)
		} else {
			thumbResult = result
		}
	}

	t.handleSuccess(thumbResult, webpResult)
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

	if len(settings.ThumbnailSizes) == 0 {
		return nil, fmt.Errorf("no thumbnail sizes configured")
	}
	size := settings.ThumbnailSizes[0]

	utils.LogIfDevf("[Pipeline] Generating thumbnail: width=%d for variant %d", size.Width, t.ThumbVariantID)

	thumbImg, err := vips.NewThumbnailFromBuffer(
		fileBytes,
		size.Width,
		0,
		vips.InterestingNone,
	)
	if err != nil {
		return nil, fmt.Errorf("thumbnail from buffer: %w", err)
	}
	defer thumbImg.Close()

	width := thumbImg.Width()
	height := thumbImg.Height()

	thumbWebp, _, err := thumbImg.ExportWebp(&vips.WebpExportParams{
		Quality:         80,
		Lossless:        false,
		ReductionEffort: 4,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export thumbnail webp: %w", err)
	}

	// 使用路径生成器生成分层路径: thumbnails/2026/02/25/hash_600.webp
	pg := generator.NewPathGenerator()
	thumbIdentifiers := pg.GenerateThumbnailIdentifiers(t.StoragePath, size.Width)
	thumbPath := thumbIdentifiers.StoragePath

	if err := t.Storage.SaveWithContext(ctx, thumbPath, bytes.NewReader(thumbWebp)); err != nil {
		return nil, fmt.Errorf("save thumbnail: %w", err)
	}

	utils.LogIfDevf("[Pipeline] Thumbnail saved: %s (%d bytes)", thumbPath, len(thumbWebp))

	return &pipelineResult{
		StoragePath: thumbPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(thumbWebp)),
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

	originWebp, _, err := originImg.ExportWebp(&vips.WebpExportParams{
		Quality:         settings.WebPQuality,
		Lossless:        false,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export webp: %w", err)
	}

	// 使用路径生成器生成分层路径: converted/webp/2026/02/25/hash.webp
	pg := generator.NewPathGenerator()
	webpIdentifiers := pg.GenerateConvertedIdentifiers(t.StoragePath, models.FormatWebP)
	originPath := webpIdentifiers.StoragePath

	if err := t.Storage.SaveWithContext(ctx, originPath, bytes.NewReader(originWebp)); err != nil {
		return nil, fmt.Errorf("save webp: %w", err)
	}

	utils.LogIfDevf("[Pipeline] WebP saved: %s (%d bytes)", originPath, len(originWebp))

	return &pipelineResult{
		StoragePath: originPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(originWebp)),
	}, nil
}

// handleFailure 处理失败
func (t *ImagePipelineTask) handleFailure(err error, thumbFailed, webpFailed bool) {
	if t.ThumbVariantID > 0 && thumbFailed {
		_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, err.Error(), true)
	}
	if t.WebPVariantID > 0 && webpFailed {
		_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, err.Error(), true)
	}
	t.deleteCacheOnTerminalState("failed")
}

// handleSuccess 处理成功
func (t *ImagePipelineTask) handleSuccess(thumbResult, webpResult *pipelineResult) {
	// 更新缩略图变体
	if t.ThumbVariantID > 0 && thumbResult != nil {
		_ = t.VariantRepo.UpdateCompleted(
			t.ThumbVariantID,
			filepath.Base(thumbResult.StoragePath),
			thumbResult.StoragePath,
			thumbResult.FileSize,
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
			webpResult.Width,
			webpResult.Height,
		)
	}

	// 更新图片状态
	_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted)

	t.deleteCacheOnTerminalState("success")
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
	}
}
