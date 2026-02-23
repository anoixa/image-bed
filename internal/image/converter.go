package image

import (
	"context"

	"github.com/anoixa/image-bed/cache"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
)

// Converter 图片转换器
type Converter struct {
	configManager *config.Manager
	variantRepo   *images.VariantRepository
	imageRepo     *images.Repository
	storage       storage.Provider
	cacheHelper   *cache.Helper
}

// NewConverter 创建转换器
func NewConverter(cm *config.Manager, variantRepo *images.VariantRepository, imageRepo *images.Repository, storage storage.Provider, cacheHelper *cache.Helper) *Converter {
	return &Converter{
		configManager: cm,
		variantRepo:   variantRepo,
		imageRepo:     imageRepo,
		storage:       storage,
		cacheHelper:   cacheHelper,
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

	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			return
		}
	}

	// 更新图片状态为 Processing（如果当前是 None 或 Failed）
	if image.VariantStatus == models.ImageVariantStatusNone || image.VariantStatus == models.ImageVariantStatusFailed {
		if err := c.imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusProcessing); err != nil {
			utils.LogIfDevf("[Converter] Failed to update image status to processing: %v", err)
			// 继续尝试，不中断流程
		} else {
			image.VariantStatus = models.ImageVariantStatusProcessing
		}
	}

	variant, err := c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to upsert variant: %v", err)
		return
	}

	if variant.Status != models.VariantStatusPending {
		utils.LogIfDevf("[Converter] Variant %d status=%s, skip submission (retry_count=%d)",
			variant.ID, variant.Status, variant.RetryCount)
		return
	}

	if variant.RetryCount >= settings.MaxRetries {
		utils.LogIfDevf("[Converter] Variant %d reached max retries (%d >= %d), skip submission",
			variant.ID, variant.RetryCount, settings.MaxRetries)
		return
	}

	// 提交任务
	pool := worker.GetGlobalPool()
	if pool == nil {
		return
	}

	ok := pool.Submit(func() {
		task := &worker.WebPConversionTask{
			VariantID:       variant.ID,
			ImageID:         image.ID,
			ImageIdentifier: image.Identifier,
			SourcePath:      image.StoragePath,
			SourceWidth:     image.Width,
			SourceHeight:    image.Height,
			ConfigManager:   c.configManager,
			VariantRepo:     c.variantRepo,
			ImageRepo:       c.imageRepo,
			Storage:         c.storage,
			CacheHelper:     c.cacheHelper,
		}
		task.Execute()
	})

	if !ok {
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

	if variant.RetryCount >= settings.MaxRetries {
		return
	}

	if err := c.variantRepo.ResetForRetry(variant.ID, 0); err != nil {
		utils.LogIfDevf("[Converter] Failed to reset variant %d: %v", variant.ID, err)
		return
	}

	// 重新触发
	c.TriggerWebPConversion(image)
}
