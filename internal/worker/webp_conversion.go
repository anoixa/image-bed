package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/h2non/bimg"
)

// ErrorType 错误类型
type ErrorType int

const (
	ErrorTransient ErrorType = iota // 可重试
	ErrorPermanent                  // 永久错误
	ErrorConfig                     // 配置错误
)

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
	identifier string
	fileSize   int64
	width      int
	height     int
}

// WebPConversionTask WebP转换任务
type WebPConversionTask struct {
	VariantID        uint
	ImageID          uint
	SourceIdentifier string
	ConfigManager    *config.Manager
	VariantRepo      VariantRepository
	Storage          storage.Provider
	result           *conversionResult
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

	// 检查 WebP 是否仍启用
	if !t.isFormatEnabled(settings, models.FormatWebP) {
		utils.LogIfDevf("[WebPConversion] WebP format disabled, skipping variant %d", t.VariantID)
		return
	}

	// CAS 获取 processing 状态
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
	utils.LogIfDevf("[WebPConversion] Starting conversion for variant %d, image=%s", t.VariantID, t.SourceIdentifier)
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
		t.VariantRepo.UpdateStatusCAS(
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
	reader, err := t.Storage.GetWithContext(ctx, t.SourceIdentifier)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read data: %w", err)
	}

	// 检查尺寸限制
	if settings.MaxDimension > 0 {
		size, err := bimg.NewImage(data).Size()
		if err == nil {
			if size.Width > settings.MaxDimension || size.Height > settings.MaxDimension {
				return fmt.Errorf("image exceeds max dimension: %dx%d", size.Width, size.Height)
			}
		}
	}

	// 转换为 WebP
	converted, err := bimg.NewImage(data).Process(bimg.Options{
		Type:    bimg.WEBP,
		Quality: settings.WebPQuality,
	})
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}

	// 保存到存储
	variantIdentifier := fmt.Sprintf("%s.webp", t.SourceIdentifier)
	if err := t.Storage.SaveWithContext(ctx, variantIdentifier, bytes.NewReader(converted)); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	// 获取尺寸
	size, _ := bimg.NewImage(converted).Size()

	// 保存结果
	t.result = &conversionResult{
		identifier: variantIdentifier,
		fileSize:   int64(len(converted)),
		width:      size.Width,
		height:     size.Height,
	}

	return nil
}

// handleSuccess 处理成功
func (t *WebPConversionTask) handleSuccess() {
	if t.result == nil {
		utils.LogIfDevf("[WebPConversion] Result is nil for variant %d", t.VariantID)
		return
	}

	utils.LogIfDevf("[WebPConversion] Updating completed status for variant %d, identifier=%s",
		t.VariantID, t.result.identifier)
	err := t.VariantRepo.UpdateCompleted(
		t.VariantID,
		t.result.identifier,
		t.result.fileSize,
		t.result.width,
		t.result.height,
	)
	if err != nil {
		// 清理已上传的文件
		ctx := context.Background()
		t.Storage.DeleteWithContext(ctx, t.result.identifier)
		utils.LogIfDevf("[WebPConversion] Failed to update completed status for variant %d: %v", t.VariantID, err)
	} else {
		utils.LogIfDevf("[WebPConversion] Successfully completed variant %d", t.VariantID)
	}
}

// handleFailure 处理失败
func (t *WebPConversionTask) handleFailure(err error) {
	errType := ClassifyError(err)
	errMsg := err.Error()

	// 更新为 failed 状态
	t.VariantRepo.UpdateStatusCAS(
		t.VariantID,
		models.VariantStatusProcessing,
		models.VariantStatusFailed,
		errMsg,
	)

	switch errType {
	case ErrorPermanent:
		utils.LogIfDevf("[WebPConversion] Permanent error for variant %d: %s", t.VariantID, errMsg)
	case ErrorConfig:
		utils.LogIfDevf("[WebPConversion] Config error for variant %d: %s", t.VariantID, errMsg)
	case ErrorTransient:
		utils.LogIfDevf("[WebPConversion] Transient error for variant %d: %s", t.VariantID, errMsg)
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

// VariantRepository 接口（避免循环依赖）
type VariantRepository interface {
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier string, fileSize int64, width, height int) error
	UpdateFailed(id uint, errMsg string, allowRetry bool) error
	GetByID(id uint) (*models.ImageVariant, error)
}
