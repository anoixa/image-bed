package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/davidbyttow/govips/v2/vips"
)

// ThumbnailTask 缩略图生成任务
type ThumbnailTask struct {
	VariantID        uint
	ImageID          uint
	SourcePath       string
	TargetPath       string
	TargetIdentifier string
	TargetWidth      int
	ConfigManager    *config.Manager
	VariantRepo      VariantRepository
	ImageRepo        ImageRepository
	Storage          storage.Provider
}

// Execute 执行任务
func (t *ThumbnailTask) Execute() {
	defer t.recovery()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	settings, err := t.ConfigManager.GetThumbnailSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[ThumbnailTask] Failed to get config: %v", err)
		return
	}

	if !settings.Enabled {
		utils.LogIfDevf("[ThumbnailTask] Thumbnail generation disabled")
		return
	}

	// CAS 获取 processing 状态
	utils.LogIfDevf("[ThumbnailTask] Attempting CAS: variant %d pending->processing", t.VariantID)
	acquired, err := t.VariantRepo.UpdateStatusCAS(
		t.VariantID,
		models.VariantStatusPending,
		models.VariantStatusProcessing,
		"",
	)
	if err != nil {
		utils.LogIfDevf("[ThumbnailTask] CAS error for variant %d: %v", t.VariantID, err)
		return
	}
	if !acquired {
		utils.LogIfDevf("[ThumbnailTask] CAS failed for variant %d, already processing", t.VariantID)
		return
	}

	utils.LogIfDevf("[ThumbnailTask] Processing variant %d: %s -> %s (width: %d)",
		t.VariantID, t.SourcePath, t.TargetPath, t.TargetWidth)

	result := t.process()

	if result.Error != nil {
		t.handleError(result.Error, settings.MaxRetries)
	} else {
		t.handleSuccess(result)
	}
}

// process 处理缩略图生成
func (t *ThumbnailTask) process() *thumbnailResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 从存储获取图片 reader
	reader, err := t.Storage.GetWithContext(ctx, t.SourcePath)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to get source image: %w", err)}
	}

	thumbnailData, width, height, err := t.generateThumbnail(reader, t.TargetWidth)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to generate thumbnail: %w", err)}
	}

	if err := t.Storage.SaveWithContext(ctx, t.TargetPath, bytes.NewReader(thumbnailData)); err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to store thumbnail: %w", err)}
	}

	return &thumbnailResult{
		Width:    width,
		Height:   height,
		FileSize: int64(len(thumbnailData)),
	}
}

// thumbnailResult 缩略图处理结果
type thumbnailResult struct {
	Width    int
	Height   int
	FileSize int64
	Error    error
}

// generateThumbnail 生成缩略图
func (t *ThumbnailTask) generateThumbnail(reader io.Reader, targetWidth int) ([]byte, int, int, error) {
	ctx := context.Background()
	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		return nil, 0, 0, fmt.Errorf("acquire semaphore: %w", err)
	}
	defer semaphore.Release()

	memBefore := utils.GetMemoryStats()
	utils.LogIfDevf("[ThumbnailTask][%d] Starting thumbnail generation, heap: %.2fMB",
		t.VariantID, memBefore.HeapAllocMB)

	const maxImageSize = 50 * 1024 * 1024 // 50MB 最大限制
	limitedReader := io.LimitReader(reader, maxImageSize)

	utils.LogIfDevf("[ThumbnailTask] Loading image for variant %d, source=%s", t.VariantID, t.SourcePath)

	img, err := vips.NewImageFromReader(limitedReader)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to load image: %w", err)
	}
	defer func() {
		img.Close()
		runtime.GC()

		memAfter := utils.GetMemoryStats()
		delta := memAfter.HeapAllocMB - memBefore.HeapAllocMB
		utils.LogIfDevf("[ThumbnailTask][%d] Image closed, heap delta: %+.2fMB (before: %.2fMB, after: %.2fMB)",
			t.VariantID, delta, memBefore.HeapAllocMB, memAfter.HeapAllocMB)
	}()

	memAfterLoad := utils.GetMemoryStats()
	utils.LogIfDevf("[ThumbnailTask] Image loaded, heap delta: +%.2fMB",
		memAfterLoad.HeapAllocMB-memBefore.HeapAllocMB)

	width := img.Width()
	height := img.Height()

	if width <= targetWidth {
		webpBytes, _, err := img.ExportWebp(&vips.WebpExportParams{
			Quality:  85,
			Lossless: false,
		})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to export webp: %w", err)
		}
		return webpBytes, width, height, nil
	}

	targetHeight := height * targetWidth / width

	err = img.Thumbnail(targetWidth, targetHeight, vips.InterestingCentre)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to thumbnail image: %w", err)
	}

	// 导出为 WebP
	webpBytes, _, err := img.ExportWebp(&vips.WebpExportParams{
		Quality:  85,
		Lossless: false,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to export webp: %w", err)
	}

	return webpBytes, targetWidth, targetHeight, nil
}

// handleSuccess 处理成功
func (t *ThumbnailTask) handleSuccess(result *thumbnailResult) {
	utils.LogIfDevf("[ThumbnailTask] Success: variant %d, %dx%d, %d bytes",
		t.VariantID, result.Width, result.Height, result.FileSize)

	_ = t.VariantRepo.UpdateCompleted(
		t.VariantID,
		t.TargetIdentifier,
		t.TargetPath,
		result.FileSize,
		result.Width,
		result.Height,
	)
}

// handleError 处理错误
func (t *ThumbnailTask) handleError(err error, maxRetries int) {
	utils.LogIfDevf("[ThumbnailTask] Error: variant %d, %v", t.VariantID, err)
	_ = t.VariantRepo.UpdateFailed(t.VariantID, err.Error(), true)
}

// recovery 恢复 panic
func (t *ThumbnailTask) recovery() {
	if rec := recover(); rec != nil {
		utils.LogIfDevf("[ThumbnailTask] Panic recovered: %v", rec)
		_, _ = t.VariantRepo.UpdateStatusCAS(
			t.VariantID,
			models.VariantStatusProcessing,
			models.VariantStatusFailed,
			fmt.Sprintf("panic: %v", rec),
		)
	}
}
