package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/anoixa/image-bed/utils/pool"
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

// detectImageComplexity detects image complexity level.
// fileSize is the compressed file size in bytes (used to estimate compression ratio).
func detectImageComplexity(img *vips.ImageRef, fileSize int64) ImageComplexity {
	hasAlpha := img.HasAlpha()
	if hasAlpha {
		return ComplexityLow
	}

	width := img.Width()
	height := img.Height()
	pixelCount := width * height

	if pixelCount == 0 || fileSize == 0 {
		return ComplexityMedium
	}

	bytesPerPixel := float64(fileSize) / float64(pixelCount)

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

// ImagePipelineTask 统一图片处理任务
type ImagePipelineTask struct {
	ThumbVariantID  uint
	WebPVariantID   uint
	ImageID         uint
	StoragePath     string
	ImageIdentifier string
	FileSize        int64  // used by detectImageComplexity instead of len(fileBytes)
	MimeType        string // used for GIF guard in generateThumbnail
	Storage         storage.Provider
	Settings        *dbconfig.ImageProcessingSettings
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	CacheHelper     *cache.Helper
}

// getProcessingFilePath returns an OS file path suitable for vips file-based APIs.
// For local storage it returns the stored file path directly (no I/O).
// For remote storage it downloads to a temp file bounded by maxSize.
// Caller must invoke the returned cleanup func exactly once via defer.
func (t *ImagePipelineTask) getProcessingFilePath(ctx context.Context) (path string, cleanup func(), err error) {
	noop := func() {}

	// Local storage: return path directly, no temp file needed
	if pp, ok := t.Storage.(storage.PathProvider); ok {
		p, e := pp.GetFilePath(t.StoragePath)
		if e != nil {
			return "", noop, fmt.Errorf("get file path: %w", e)
		}
		return p, noop, nil
	}

	// Remote storage: download to temp file
	maxSize := int64(50) * 1024 * 1024
	if t.Settings != nil && t.Settings.MaxFileSizeMB > 0 {
		maxSize = int64(t.Settings.MaxFileSizeMB) * 1024 * 1024
	}

	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return "", noop, fmt.Errorf("create temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(config.TempDir, "pipeline-proc-*")
	if err != nil {
		return "", noop, fmt.Errorf("create temp file: %w", err)
	}

	cleanupFn := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}

	stream, err := t.Storage.GetWithContext(ctx, t.StoragePath)
	if err != nil {
		cleanupFn()
		return "", noop, fmt.Errorf("get stream: %w", err)
	}
	if closer, ok := stream.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	lr := io.LimitReader(stream, maxSize+1)
	n, copyErr := io.CopyBuffer(tmp, lr, *bufPtr)
	if copyErr != nil {
		cleanupFn()
		return "", noop, fmt.Errorf("write temp file: %w", copyErr)
	}
	if n > maxSize {
		cleanupFn()
		return "", noop, fmt.Errorf("file exceeds max size %d bytes", maxSize)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) // avoid double-close; file may already be partially closed
		return "", noop, fmt.Errorf("close temp file: %w", err)
	}

	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

// Execute 执行任务
func (t *ImagePipelineTask) Execute() {
	defer t.recovery()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

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
func (t *ImagePipelineTask) runPipeline(ctx context.Context) error {
	filePath, cleanup, err := t.getProcessingFilePath(ctx)
	if err != nil {
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("get file: %v", err), true)
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("get file: %v", err), true)
		}
		return fmt.Errorf("get processing file: %w", err)
	}
	defer cleanup()

	var thumbResult, webpResult *pipelineResult
	var hasSuccess, hasFailed bool

	if t.ThumbVariantID > 0 {
		result, err := t.generateThumbnail(ctx, filePath)
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

	if t.WebPVariantID > 0 {
		result, err := t.generateWebP(ctx, filePath)
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

	_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted)
	t.deleteCacheOnTerminalState("success")
	return nil
}

// generateThumbnail 生成缩略图
func (t *ImagePipelineTask) generateThumbnail(ctx context.Context, filePath string) (*pipelineResult, error) {
	settings := t.Settings
	if settings == nil {
		return nil, fmt.Errorf("image processing settings not provided")
	}
	if !settings.ThumbnailEnabled {
		utils.LogIfDevf("[Pipeline] Thumbnail generation disabled")
		return nil, nil
	}

	// GIF guard: converter.go already blocks GIFs before task creation;
	// this is defense-in-depth using the stored MIME type.
	if t.MimeType == "image/gif" {
		utils.LogIfDevf("[Pipeline] Skipping GIF thumbnail generation")
		return nil, nil
	}

	if len(settings.ThumbnailSizes) == 0 {
		return nil, fmt.Errorf("no thumbnail sizes configured")
	}
	size := settings.ThumbnailSizes[0]

	utils.LogIfDevf("[Pipeline] Generating thumbnail: width=%d for variant %d", size.Width, t.ThumbVariantID)

	thumbImg, err := vips.NewThumbnailFromFile(filePath, size.Width, -1, vips.InterestingNone)
	if err != nil {
		return nil, fmt.Errorf("thumbnail from file: %w", err)
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
func (t *ImagePipelineTask) generateWebP(ctx context.Context, filePath string) (*pipelineResult, error) {
	settings := t.Settings
	if settings == nil {
		return nil, fmt.Errorf("image processing settings not provided")
	}

	return t.generateWebPWithSettings(ctx, filePath, settings)
}

// generateWebPWithSettings 使用指定设置生成 WebP
func (t *ImagePipelineTask) generateWebPWithSettings(ctx context.Context, filePath string, settings *dbconfig.ImageProcessingSettings) (*pipelineResult, error) {
	if !settings.IsFormatEnabled(models.FormatWebP) {
		utils.LogIfDevf("[Pipeline] WebP format disabled")
		return nil, nil
	}

	utils.LogIfDevf("[Pipeline] Generating WebP for variant %d", t.WebPVariantID)

	originImg, err := vips.NewImageFromFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("load image from file: %w", err)
	}
	defer originImg.Close()

	width := originImg.Width()
	height := originImg.Height()
	if settings.MaxDimension > 0 {
		if width > settings.MaxDimension || height > settings.MaxDimension {
			return nil, fmt.Errorf("image exceeds max dimension: %dx%d", width, height)
		}
	}

	complexity := detectImageComplexity(originImg, t.FileSize)
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

		utils.LogIfDevf("[Pipeline] Panic during processing for image %s; orphaned storage files (if any) can be cleaned with the 'clean' command", t.ImageIdentifier)
	}
}
