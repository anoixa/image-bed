package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
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
	SourcePath       string // 原图存储路径
	TargetPath       string // 缩略图存储路径
	TargetIdentifier string // 缩略图标识符
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

	// 读取配置
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

	// 执行转换
	result := t.process()

	// 更新状态
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

	// 生成缩略图
	thumbnailData, width, height, err := t.generateThumbnail(reader, t.TargetWidth)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to generate thumbnail: %w", err)}
	}

	// 存储缩略图
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
	// 获取信号量，限制并发处理的图片数量
	ctx := context.Background()
	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		return nil, 0, 0, fmt.Errorf("acquire semaphore: %w", err)
	}
	defer semaphore.Release()

	// 记录处理前的内存状态
	memBefore := utils.GetMemoryStats()
	utils.LogIfDevf("[ThumbnailTask][%d] Starting thumbnail generation, heap: %.2fMB",
		t.VariantID, memBefore.HeapAllocMB)

	// 限制内存使用，避免大图片导致 OOM
	const maxImageSize = 50 * 1024 * 1024 // 50MB 最大限制
	limitedReader := io.LimitReader(reader, maxImageSize)

	utils.LogIfDevf("[ThumbnailTask] Loading image for variant %d, source=%s", t.VariantID, t.SourcePath)

	// 使用 govips 从 reader 加载图片
	img, err := vips.NewImageFromReader(limitedReader)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to load image: %w", err)
	}
	defer func() {
		img.Close()
		// 强制 GC 并等待完成
		runtime.GC()
		// 释放内存给操作系统
		debug.FreeOSMemory()
		// 记录处理后的内存状态
		memAfter := utils.GetMemoryStats()
		delta := memAfter.HeapAllocMB - memBefore.HeapAllocMB
		utils.LogIfDevf("[ThumbnailTask][%d] Image closed, heap delta: %+.2fMB (before: %.2fMB, after: %.2fMB)",
			t.VariantID, delta, memBefore.HeapAllocMB, memAfter.HeapAllocMB)
	}()

	// 打印加载后的内存状态
	memAfterLoad := utils.GetMemoryStats()
	utils.LogIfDevf("[ThumbnailTask] Image loaded, heap delta: +%.2fMB",
		memAfterLoad.HeapAllocMB-memBefore.HeapAllocMB)

	// 获取图片尺寸
	width := img.Width()
	height := img.Height()

	// 如果图片宽度小于等于目标宽度，直接转换为 WebP
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

	// 计算目标高度保持比例
	targetHeight := height * targetWidth / width

	// 使用 Thumbnail 调整尺寸（会直接修改当前图片对象）
	// 注意：govips 的 Thumbnail 方法会直接修改 img 对象
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
	// 这里需要更新变体状态
	utils.LogIfDevf("[ThumbnailTask] Success: variant %d, %dx%d, %d bytes",
		t.VariantID, result.Width, result.Height, result.FileSize)

	// 更新变体状态为完成
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
	// 简化处理
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
