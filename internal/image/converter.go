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

	utils.LogIfDevf("[Converter] TriggerConversion called for image %s (mime=%s, size=%d)",
		image.Identifier, image.MimeType, image.FileSize)

	settings, err := c.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[Converter] Failed to get settings: %v", err)
		return
	}

	utils.LogIfDevf("[Converter] Settings: WebP enabled=%v, Thumbnail enabled=%v, SkipSmallerThan=%d",
		settings.IsFormatEnabled(models.FormatWebP),
		settings.ThumbnailEnabled,
		settings.SkipSmallerThan)

	// 检查是否启用 WebP 转换
	if !settings.IsFormatEnabled(models.FormatWebP) {
		utils.LogIfDevf("[Converter] WebP format disabled, skipping")
		return
	}

	// 跳过 GIF 格式
	if image.MimeType == "image/gif" {
		utils.LogIfDevf("[Converter] Skipping GIF format")
		return
	}

	// 跳过小于阈值的图片
	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			utils.LogIfDevf("[Converter] Skipping image smaller than threshold: %d < %d (threshold from config)", image.FileSize, minSize)
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

	// 创建缩略图变体记录（如果启用）
	var thumbVariant *models.ImageVariant
	if settings.ThumbnailEnabled && len(settings.ThumbnailSizes) > 0 {
		size := settings.ThumbnailSizes[0]
		thumbFormat := models.FormatThumbnailSize(size.Width)
		thumbVariant, err = c.variantRepo.UpsertPending(image.ID, thumbFormat)
		if err != nil {
			utils.LogIfDevf("[Converter] Failed to upsert thumbnail variant: %v", err)
			return
		}
		if thumbVariant.Status != models.VariantStatusPending {
			utils.LogIfDevf("[Converter] Thumbnail variant %d status=%s, skip", thumbVariant.ID, thumbVariant.Status)
			thumbVariant = nil
		}
	}

	// 创建 WebP 变体记录（如果启用）
	var webpVariant *models.ImageVariant
	if settings.IsFormatEnabled(models.FormatWebP) {
		webpVariant, err = c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
		if err != nil {
			utils.LogIfDevf("[Converter] Failed to upsert WebP variant: %v", err)
			return
		}
		if webpVariant.Status != models.VariantStatusPending {
			utils.LogIfDevf("[Converter] WebP variant %d status=%s, skip", webpVariant.ID, webpVariant.Status)
			webpVariant = nil
		}
		if webpVariant != nil && webpVariant.RetryCount >= settings.MaxRetries {
			utils.LogIfDevf("[Converter] WebP variant %d reached max retries", webpVariant.ID)
			webpVariant = nil
		}
	}

	// 如果没有需要处理的变体，直接返回
	if thumbVariant == nil && webpVariant == nil {
		utils.LogIfDevf("[Converter] No pending variants for %s, skip", image.Identifier)
		return
	}

	pool := worker.GetGlobalPool()
	if pool == nil {
		return
	}

	// 提交统一流水线任务
	ok := pool.Submit(func() {
		task := &worker.ImagePipelineTask{
			ThumbVariantID:  getVariantID(thumbVariant),
			WebPVariantID:   getVariantID(webpVariant),
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

// getVariantID 辅助函数：从变体指针获取ID
func getVariantID(v *models.ImageVariant) uint {
	if v == nil {
		return 0
	}
	return v.ID
}

// TriggerRetry 触发指定变体的重试
func (c *Converter) TriggerRetry(variant *models.ImageVariant, image *models.Image) {
	ctx := context.Background()
	settings, err := c.configManager.GetImageProcessingSettings(ctx)
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
