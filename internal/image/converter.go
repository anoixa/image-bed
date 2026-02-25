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

// TriggerConversion 触发图片转换（统一流水线）
// 使用 PipelineTask 同时生成缩略图和 WebP 原图
func (c *Converter) TriggerConversion(image *models.Image) {
	ctx := context.Background()

	settings, err := c.configManager.GetConversionSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to get settings: %v", err)
		return
	}

	// 检查是否启用任意转换
	if !settings.IsFormatEnabled(models.FormatWebP) {
		return
	}

	// 跳过 GIF 格式
	if image.MimeType == "image/gif" {
		return
	}

	// 跳过小于阈值的图片
	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			return
		}
	}

	// 更新图片状态为处理中
	if image.VariantStatus == models.ImageVariantStatusNone || image.VariantStatus == models.ImageVariantStatusFailed {
		if err := c.imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusProcessing); err != nil {
			utils.LogIfDevf("[Converter] Failed to update image status: %v", err)
		} else {
			image.VariantStatus = models.ImageVariantStatusProcessing
		}
	}

	// 创建 WebP 变体记录
	variant, err := c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to upsert variant: %v", err)
		return
	}

	if variant.Status != models.VariantStatusPending {
		utils.LogIfDevf("[Converter] Variant %d status=%s, skip submission", variant.ID, variant.Status)
		return
	}

	if variant.RetryCount >= settings.MaxRetries {
		utils.LogIfDevf("[Converter] Variant %d reached max retries", variant.ID)
		return
	}

	pool := worker.GetGlobalPool()
	if pool == nil {
		return
	}

	// 提交统一流水线任务
	ok := pool.Submit(func() {
		task := &worker.ImagePipelineTask{
			VariantID:       variant.ID,
			ImageID:         image.ID,
			StoragePath:     image.StoragePath,
			ImageIdentifier: image.Identifier,
			Storage:         c.storage,
			ConfigManager:   c.configManager,
			VariantRepo:     c.variantRepo,
			ImageRepo:       c.imageRepo,
			CacheHelper:     c.cacheHelper,
		}
		task.Execute()
	})

	if !ok {
		utils.LogIfDevf("[Converter] Failed to submit pipeline task for %s", image.Identifier)
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

	// 重新触发统一转换
	c.TriggerConversion(image)
}
