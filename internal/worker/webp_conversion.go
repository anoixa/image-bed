package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/anoixa/image-bed/cache"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/davidbyttow/govips/v2/vips"
)

const (
	ErrorTransient ErrorType = iota // 可重试
	ErrorPermanent                  // 永久错误
	ErrorConfig
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

	if strings.Contains(errStr, "invalid quality") ||
		strings.Contains(errStr, "quality out of range") ||
		strings.Contains(errStr, "effort out of range") {
		return ErrorConfig
	}

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
	ImageIdentifier string
	SourcePath      string
	SourceWidth     int
	SourceHeight    int
	ConfigManager   *config.Manager
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	Storage         storage.Provider
	CacheHelper     *cache.Helper
	result          *conversionResult
}

// Execute 执行任务
func (t *WebPConversionTask) Execute() {
	defer t.recovery()

	ctx := context.Background()

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

	utils.LogIfDevf("[WebPConversion] Starting conversion for variant %d, image=%s", t.VariantID, t.SourcePath)
	err = t.doConversionWithTimeout(ctx, settings)

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
	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		return fmt.Errorf("acquire semaphore: %w", err)
	}
	defer semaphore.Release()

	memBefore := utils.GetMemoryStats()
	utils.LogIfDevf("[WebPConversion][%d] Starting conversion, heap: %.2fMB",
		t.VariantID, memBefore.HeapAllocMB)

	reader, err := t.Storage.GetWithContext(ctx, t.SourcePath)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	const maxMemorySize = 10 * 1024 * 1024

	// 限制读取大小
	limitedReader := io.LimitReader(reader, maxMemorySize*5)

	utils.LogIfDevf("[WebPConversion] Loading image for variant %d, source=%s", t.VariantID, t.SourcePath)

	img, err := vips.NewImageFromReader(limitedReader)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}
	defer func() {
		img.Close()
		runtime.GC()

		memAfter := utils.GetMemoryStats()
		delta := memAfter.HeapAllocMB - memBefore.HeapAllocMB
		utils.LogIfDevf("[WebPConversion][%d] Image closed, heap delta: %+.2fMB (before: %.2fMB, after: %.2fMB)",
			t.VariantID, delta, memBefore.HeapAllocMB, memAfter.HeapAllocMB)
	}()

	memAfterLoad := utils.GetMemoryStats()
	utils.LogIfDevf("[WebPConversion] Image loaded, heap delta: +%.2fMB",
		memAfterLoad.HeapAllocMB-memBefore.HeapAllocMB)

	width := img.Width()
	height := img.Height()

	// 记录大图片警告
	if width*height > 10000*10000 {
		utils.LogIfDevf("[WebPConversion] Processing large image: %dx%d, path: %s", width, height, t.SourcePath)
	}

	if settings.MaxDimension > 0 {
		if width > settings.MaxDimension || height > settings.MaxDimension {
			return fmt.Errorf("image exceeds max dimension: %dx%d", width, height)
		}
	}

	// 导出为 WebP
	webpBytes, _, err := img.ExportWebp(&vips.WebpExportParams{
		Quality:         settings.WebPQuality,
		Lossless:        false,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return fmt.Errorf("export webp: %w", err)
	}

	pathGen := generator.NewPathGenerator()
	ids := pathGen.GenerateConvertedIdentifiers(t.SourcePath, models.FormatWebP)

	if err := t.Storage.SaveWithContext(ctx, ids.StoragePath, bytes.NewReader(webpBytes)); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	t.result = &conversionResult{
		identifier:  ids.Identifier,
		storagePath: ids.StoragePath,
		fileSize:    int64(len(webpBytes)),
		width:       width,
		height:      height,
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

	if err := t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted); err != nil {
		utils.LogIfDevf("[WebPConversion] Failed to update image status: %v", err)
	}

	t.deleteCacheOnTerminalState("success")
}

// handleFailure 处理失败
func (t *WebPConversionTask) handleFailure(err error) {
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
