package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anoixa/image-bed/cache"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/h2non/bimg"
)

const (
	ErrorTransient ErrorType = iota // 可重试
	ErrorPermanent                  // 永久错误
	ErrorConfig                     // 配置错误
)

// ErrorType 错误类型
type ErrorType int

// VariantRepository 接口
type VariantRepository interface {
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, width, height int) error
	UpdateFailed(id uint, errMsg string, allowRetry bool) error
	GetByID(id uint) (*models.ImageVariant, error)
}

// ImageRepository 接口
type ImageRepository interface {
	UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error
	GetImageByID(id uint) (*models.Image, error)
}

// ClassifyError 分类错误类型
func ClassifyError(err error) ErrorType {
	errStr := err.Error()

	// 永久错误
	if strings.Contains(errStr, "unsupported format") ||
		strings.Contains(errStr, "image corrupt") ||
		strings.Contains(errStr, "invalid image") ||
		strings.Contains(errStr, "cannot decode") {
		return ErrorPermanent
	}

	// 配置错误
	if strings.Contains(errStr, "invalid quality") ||
		strings.Contains(errStr, "quality out of range") ||
		strings.Contains(errStr, "effort out of range") {
		return ErrorConfig
	}

	// 默认可重试
	return ErrorTransient
}

// conversionResult 转换结果
type conversionResult struct {
	identifier  string
	storagePath string
	fileSize    int64
	width       int
	height      int
}

// WebPConversionTask WebP转换任务
type WebPConversionTask struct {
	VariantID       uint
	ImageID         uint
	ImageIdentifier string // 原图标识符，用于缓存失效
	SourcePath      string // 原图存储路径
	SourceWidth     int
	SourceHeight    int
	ConfigManager   *config.Manager
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	Storage         storage.Provider
	CacheHelper     *cache.Helper // 缓存帮助器，用于任务完成后删除缓存
	result          *conversionResult
}

// Execute 执行任务
func (t *WebPConversionTask) Execute() {
	defer t.recovery()

	ctx := context.Background()

	// 读取配置
	settings, err := t.ConfigManager.GetConversionSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[WebPConversion] Failed to get config: %v", err)
		return
	}
	if !t.isFormatEnabled(settings, models.FormatWebP) {
		utils.LogIfDevf("[WebPConversion] WebP format disabled, skipping variant %d", t.VariantID)
		return
	}

	utils.LogIfDevf("[WebPConversion] Attempting CAS: variant %d pending->processing", t.VariantID)
	acquired, err := t.VariantRepo.UpdateStatusCAS(
		t.VariantID,
		models.VariantStatusPending,
		models.VariantStatusProcessing,
		"",
	)
	if err != nil {
		utils.LogIfDevf("[WebPConversion] CAS error for variant %d: %v", t.VariantID, err)
		return
	}
	if !acquired {
		utils.LogIfDevf("[WebPConversion] CAS failed for variant %d (not in pending state)", t.VariantID)
		return
	}
	utils.LogIfDevf("[WebPConversion] CAS success: variant %d is now processing", t.VariantID)

	// 执行转换
	utils.LogIfDevf("[WebPConversion] Starting conversion for variant %d, image=%s", t.VariantID, t.SourcePath)
	err = t.doConversionWithTimeout(ctx, settings)

	// 处理结果
	if err != nil {
		utils.LogIfDevf("[WebPConversion] Conversion failed for variant %d: %v", t.VariantID, err)
		t.handleFailure(err)
	} else {
		utils.LogIfDevf("[WebPConversion] Conversion success for variant %d", t.VariantID)
		t.handleSuccess()
	}
}

// recovery 恢复 panic
func (t *WebPConversionTask) recovery() {
	if rec := recover(); rec != nil {
		utils.LogIfDevf("[WebPConversion] Panic recovered: %v", rec)
		_, _ = t.VariantRepo.UpdateStatusCAS(
			t.VariantID,
			models.VariantStatusProcessing,
			models.VariantStatusFailed,
			fmt.Sprintf("panic: %v", rec),
		)
	}
}

// doConversionWithTimeout 带超时的转换
func (t *WebPConversionTask) doConversionWithTimeout(ctx context.Context, settings *config.ConversionSettings) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- t.doConversion(ctx, settings)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("conversion timeout")
	}
}

// doConversion 执行转换
func (t *WebPConversionTask) doConversion(ctx context.Context, settings *config.ConversionSettings) error {
	// 读取原图
	reader, err := t.Storage.GetWithContext(ctx, t.SourcePath)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read data: %w", err)
	}

	if settings.MaxDimension > 0 {
		width, height := t.SourceWidth, t.SourceHeight
		if width == 0 || height == 0 {
			// fallback: 从数据中解析
			size, err := bimg.NewImage(data).Size()
			if err == nil {
				width, height = size.Width, size.Height
			}
		}
		if width > settings.MaxDimension || height > settings.MaxDimension {
			return fmt.Errorf("image exceeds max dimension: %dx%d", width, height)
		}
	}

	converted, err := bimg.NewImage(data).Process(bimg.Options{
		Type:    bimg.WEBP,
		Quality: settings.WebPQuality,
	})
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}

	// 使用 PathGenerator 生成存储路径
	pathGen := generator.NewPathGenerator()
	ids := pathGen.GenerateConvertedIdentifiers(t.SourcePath, models.FormatWebP)

	// 存储转换后的文件
	if err := t.Storage.SaveWithContext(ctx, ids.StoragePath, bytes.NewReader(converted)); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	// 获取图片尺寸
	size, err := bimg.NewImage(converted).Size()
	if err != nil {
		return fmt.Errorf("get size: %w", err)
	}

	t.result = &conversionResult{
		identifier:  ids.Identifier,
		storagePath: ids.StoragePath,
		fileSize:    int64(len(converted)),
		width:       size.Width,
		height:      size.Height,
	}

	return nil
}

// handleSuccess 处理成功
func (t *WebPConversionTask) handleSuccess() {
	if t.result == nil {
		return
	}

	if err := t.VariantRepo.UpdateCompleted(
		t.VariantID,
		t.result.identifier,
		t.result.storagePath,
		t.result.fileSize,
		t.result.width,
		t.result.height,
	); err != nil {
		utils.LogIfDevf("[WebPConversion] Failed to update completed status: %v", err)
		return
	}

	// 更新图片的变体状态
	if err := t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted); err != nil {
		utils.LogIfDevf("[WebPConversion] Failed to update image status: %v", err)
	}

	// 删除缓存
	t.deleteCacheOnTerminalState("success")
}

// handleFailure 处理失败
func (t *WebPConversionTask) handleFailure(err error) {
	// 获取当前变体信息
	variant, getErr := t.VariantRepo.GetByID(t.VariantID)
	if getErr != nil {
		utils.LogIfDevf("[WebPConversion] Failed to get variant for error handling: %v", getErr)
		return
	}

	// 判断是否允许重试
	allowRetry := variant.RetryCount < 3
	errType := ClassifyError(err)

	switch errType {
	case ErrorPermanent:
		// 永久错误，标记为失败，不重试
		allowRetry = false
	case ErrorConfig:
		// 配置错误，可能是设置问题，也不重试
		allowRetry = false
	default:
		// 可重试错误
	}

	if err := t.VariantRepo.UpdateFailed(t.VariantID, err.Error(), allowRetry); err != nil {
		utils.LogIfDevf("[WebPConversion] Failed to update failed status: %v", err)
	}

	if !allowRetry {
		t.deleteCacheOnTerminalState("failed")
	}
}

// deleteCacheOnTerminalState 删除缓存
func (t *WebPConversionTask) deleteCacheOnTerminalState(state string) {
	if t.CacheHelper != nil && t.ImageIdentifier != "" {
		ctx := context.Background()
		if err := t.CacheHelper.DeleteCachedImage(ctx, t.ImageIdentifier); err != nil {
			utils.LogIfDevf("[WebPConversion] Failed to delete image cache for %s on %s: %v", t.ImageIdentifier, state, err)
		} else {
			utils.LogIfDevf("[WebPConversion] Deleted image cache for %s after %s", t.ImageIdentifier, state)
		}
	}
}

// isFormatEnabled 检查格式是否启用
func (t *WebPConversionTask) isFormatEnabled(settings *config.ConversionSettings, format string) bool {
	for _, f := range settings.EnabledFormats {
		if f == format {
			return true
		}
	}
	return false
}
