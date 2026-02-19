package worker

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/h2non/bimg"
)

// ThumbnailTask 缩略图生成任务
type ThumbnailTask struct {
	VariantID        uint
	ImageID          uint
	SourceIdentifier string
	TargetIdentifier string
	TargetWidth      int
	ConfigManager    *config.Manager
	VariantRepo      VariantRepository
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

	// 检查缩略图功能是否启用
	if !settings.Enabled {
		utils.LogIfDevf("[ThumbnailTask] Thumbnail disabled, skipping variant %d", t.VariantID)
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
		t.VariantID, t.SourceIdentifier, t.TargetIdentifier, t.TargetWidth)

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
	imageData, err := t.getImageData(t.SourceIdentifier)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to get source image: %w", err)}
	}

	// 生成缩略图
	thumbnailData, width, height, err := t.generateThumbnail(imageData, t.TargetWidth)
	if err != nil {
		return &thumbnailResult{Error: fmt.Errorf("failed to generate thumbnail: %w", err)}
	}

	// 存储缩略图
	if err := t.Storage.SaveWithContext(ctx, t.TargetIdentifier, bytes.NewReader(thumbnailData)); err != nil {
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
func (t *ThumbnailTask) getImageData(identifier string) ([]byte, error) {
	ctx := context.Background()
	reader, err := t.Storage.GetWithContext(ctx, identifier)
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

	// 如果图片宽度已经小于目标宽度，直接返回原图（但转换为 WebP）
	if size.Width <= targetWidth {
		webpData, err := img.Convert(bimg.WEBP)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to convert to WebP: %w", err)
		}
		return webpData, size.Width, size.Height, nil
	}

	// 计算新高度，保持比例
	newHeight := int(float64(targetWidth) * float64(size.Height) / float64(size.Width))

	// 设置处理选项（使用 WebP 格式）
	options := bimg.Options{
		Width:   targetWidth,
		Height:  newHeight,
		Crop:    false,
		Quality: 85,
		Type:    bimg.WEBP,
	}

	// 处理图片
	processed, err := img.Process(options)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to process image: %w", err)
	}

	return processed, targetWidth, newHeight, nil
}

// handleSuccess 处理成功
func (t *ThumbnailTask) handleSuccess(result *thumbnailResult) {
	utils.LogIfDevf("[ThumbnailTask] Success: variant %d (%dx%d, %d bytes)",
		t.VariantID, result.Width, result.Height, result.FileSize)

	if err := t.VariantRepo.UpdateCompleted(t.VariantID, t.TargetIdentifier, result.FileSize, result.Width, result.Height); err != nil {
		utils.LogIfDevf("[ThumbnailTask] Failed to update status: %v", err)
	}
}

// handleError 处理错误
func (t *ThumbnailTask) handleError(err error, maxRetries int) {
	utils.LogIfDevf("[ThumbnailTask] Error: variant %d - %v", t.VariantID, err)

	errorType := ClassifyError(err)
	shouldRetry := errorType == ErrorTransient && t.VariantID > 0

	if shouldRetry {
		variant, getErr := t.VariantRepo.GetByID(t.VariantID)
		if getErr != nil {
			utils.LogIfDevf("[ThumbnailTask] Failed to get variant for retry check: %v", getErr)
			shouldRetry = false
		} else if variant.RetryCount >= maxRetries {
			utils.LogIfDevf("[ThumbnailTask] Max retries reached for variant %d", t.VariantID)
			shouldRetry = false
		}
	}

	if shouldRetry {
		t.VariantRepo.UpdateFailed(t.VariantID, err.Error(), true)
	} else {
		t.VariantRepo.UpdateFailed(t.VariantID, err.Error(), false)
	}
}

// recovery 异常恢复
func (t *ThumbnailTask) recovery() {
	if r := recover(); r != nil {
		utils.LogIfDevf("[ThumbnailTask] Panic recovered: %v", r)
		if t.VariantID > 0 {
			t.VariantRepo.UpdateFailed(t.VariantID, fmt.Sprintf("panic: %v", r), true)
		}
	}
}
