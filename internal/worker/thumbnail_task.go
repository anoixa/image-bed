package worker

import (
	"bytes"
	"context"
	"fmt"
	"time"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/h2non/bimg"
)

// ThumbnailTask 缩略图生成任务
type ThumbnailTask struct {
	VariantID       uint
	ImageID         uint
	SourcePath      string // 原图存储路径
	TargetPath      string // 缩略图存储路径
	TargetIdentifier string // 缩略图标识符
	TargetWidth     int
	ConfigManager   *config.Manager
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	Storage         storage.Provider
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

	// 读取原图数据
	imageData, err := t.getImageData(t.SourcePath)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to get source image: %w", err)}
	}

	// 生成缩略图
	thumbnailData, width, height, err := t.generateThumbnail(imageData, t.TargetWidth)
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

// getImageData 获取图片数据
func (t *ThumbnailTask) getImageData(storagePath string) ([]byte, error) {
	ctx := context.Background()
	reader, err := t.Storage.GetWithContext(ctx, storagePath)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// generateThumbnail 生成缩略图
func (t *ThumbnailTask) generateThumbnail(data []byte, targetWidth int) ([]byte, int, int, error) {
	// 使用 bimg (libvips) 进行图片处理
	img := bimg.NewImage(data)

	// 获取图片尺寸
	size, err := img.Size()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to get image size: %w", err)
	}

	if size.Width <= targetWidth {
		webpData, err := img.Convert(bimg.WEBP)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to convert to WebP: %w", err)
		}
		return webpData, size.Width, size.Height, nil
	}

	// 计算高度保持比例
	targetHeight := size.Height * targetWidth / size.Width

	// 调整尺寸并转换为 WebP
	processed, err := img.Process(bimg.Options{
		Width:   targetWidth,
		Height:  targetHeight,
		Type:    bimg.WEBP,
		Quality: 85,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to resize and convert: %w", err)
	}

	return processed, targetWidth, targetHeight, nil
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
