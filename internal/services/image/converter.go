package image

import (
	"context"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/internal/worker"
)

// Converter 图片转换器
type Converter struct {
	configManager *config.Manager
	variantRepo   images.VariantRepository
	storage       storage.Provider
}

// NewConverter 创建转换器
func NewConverter(cm *config.Manager, repo images.VariantRepository, storage storage.Provider) *Converter {
	return &Converter{
		configManager: cm,
		variantRepo:   repo,
		storage:       storage,
	}
}

// TriggerWebPConversion 触发 WebP 转换
// 用于：1.上传新图片 2.重试失败任务 3.用户访问时按需触发
func (c *Converter) TriggerWebPConversion(image *models.Image) {
	ctx := context.Background()

	// 读取配置
	settings, err := c.configManager.GetConversionSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to get settings: %v", err)
		return
	}

	// 检查 WebP 是否启用
	if !settings.IsFormatEnabled(models.FormatWebP) {
		return
	}

	// 检查大小限制
	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			return
		}
	}

	// 创建或获取变体记录
	variant, err := c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to upsert variant: %v", err)
		return
	}

	// 只有 pending 状态才提交任务
	if variant.Status != models.VariantStatusPending {
		utils.LogIfDevf("[Converter] Variant %d status=%s, skip submission (retry_count=%d)",
			variant.ID, variant.Status, variant.RetryCount)
		return
	}

	// 检查重试次数
	if variant.RetryCount >= settings.MaxRetries {
		utils.LogIfDevf("[Converter] Variant %d reached max retries (%d >= %d), skip submission",
			variant.ID, variant.RetryCount, settings.MaxRetries)
		return
	}

	// 提交任务
	task := &worker.WebPConversionTask{
		VariantID:        variant.ID,
		ImageID:          image.ID,
		SourceIdentifier: image.Identifier,
		ConfigManager:    c.configManager,
		VariantRepo:      c.variantRepo,
		Storage:          c.storage,
	}

	if !worker.TrySubmit(task, 3, 100*time.Millisecond) {
		utils.LogIfDevf("[Converter] Failed to submit task for %s", image.Identifier)
	}
}

// TriggerRetry 触发指定变体的重试
func (c *Converter) TriggerRetry(variant *models.ImageVariant, image *models.Image) {
	ctx := context.Background()
	settings, err := c.configManager.GetConversionSettings(ctx)
	if err != nil {
		return
	}

	// 检查重试次数
	if variant.RetryCount >= settings.MaxRetries {
		return
	}

	// CAS 重置为 pending
	if err := c.variantRepo.ResetForRetry(variant.ID, 0); err != nil {
		utils.LogIfDevf("[Converter] Failed to reset variant %d: %v", variant.ID, err)
		return
	}

	// 重新触发
	c.TriggerWebPConversion(image)
}
