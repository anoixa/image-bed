package image

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/cache"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/vipsfile"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
)

var converterLog = utils.ForModule("Converter")

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
// 使用 PipelineTask 顺序生成缩略图、WebP 和 AVIF。
func (c *Converter) TriggerConversion(image *models.Image) {
	ctx := context.Background()
	now := time.Now()

	converterLog.Debugf("TriggerConversion called for image %s (mime=%s, size=%d)",
		image.Identifier, image.MimeType, image.FileSize)

	settings, err := c.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		converterLog.Debugf("Failed to get settings: %v", err)
		return
	}

	converterLog.Debugf("Settings: WebP enabled=%v, Thumbnail enabled=%v, SkipSmallerThan=%d",
		settings.IsFormatEnabled(models.FormatWebP),
		settings.ThumbnailEnabled,
		settings.SkipSmallerThan)

	thumbnailEnabled := settings.ThumbnailEnabled && len(settings.ThumbnailSizes) > 0
	webpEnabled := settings.IsFormatEnabled(models.FormatWebP)
	avifEnabled := settings.IsFormatEnabled(models.FormatAVIF) && vipsfile.SupportsAVIFEncoding()
	if !shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled) {
		converterLog.Debugf("All variant generation disabled, skipping")
		return
	}

	// 跳过 GIF 格式
	if image.MimeType == "image/gif" {
		converterLog.Debugf("Skipping GIF format")
		return
	}

	// 跳过小于阈值的图片
	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			converterLog.Debugf("Skipping image smaller than threshold: %d < %d (threshold from config)", image.FileSize, minSize)
			return
		}
	}

	// 创建缩略图变体记录（如果启用）
	var thumbVariant *models.ImageVariant
	if thumbnailEnabled {
		size := settings.ThumbnailSizes[0]
		thumbFormat := models.FormatThumbnailSize(size.Width)
		thumbVariant, err = c.variantRepo.UpsertPending(image.ID, thumbFormat)
		if err != nil {
			converterLog.Debugf("Failed to upsert thumbnail variant: %v", err)
			return
		}
		if !variantReadyForSubmit(thumbVariant, now) {
			converterLog.Debugf("Thumbnail variant %d not ready for submit (status=%s, next_retry_at=%v), skip", thumbVariant.ID, thumbVariant.Status, thumbVariant.NextRetryAt)
			thumbVariant = nil
		}
	}

	// 创建 WebP 变体记录（如果启用）
	var webpVariant *models.ImageVariant
	if webpEnabled {
		webpVariant, err = c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
		if err != nil {
			converterLog.Debugf("Failed to upsert WebP variant: %v", err)
			c.failPendingVariantsOnSubmitFailure(image, fmt.Sprintf("submit aborted during webp preparation: %v", err), thumbVariant)
			return
		}
		if !variantReadyForSubmit(webpVariant, now) {
			converterLog.Debugf("WebP variant %d not ready for submit (status=%s, next_retry_at=%v), skip", webpVariant.ID, webpVariant.Status, webpVariant.NextRetryAt)
			webpVariant = nil
		}
	}

	var avifVariant *models.ImageVariant
	if avifEnabled {
		avifVariant, err = c.variantRepo.UpsertPending(image.ID, models.FormatAVIF)
		if err != nil {
			converterLog.Debugf("Failed to upsert AVIF variant: %v", err)
			c.failPendingVariantsOnSubmitFailure(image, fmt.Sprintf("submit aborted during avif preparation: %v", err), thumbVariant, webpVariant)
			return
		}
		if !variantReadyForSubmit(avifVariant, now) {
			converterLog.Debugf("AVIF variant %d not ready for submit (status=%s, next_retry_at=%v), skip", avifVariant.ID, avifVariant.Status, avifVariant.NextRetryAt)
			avifVariant = nil
		}
	}

	// 如果没有需要处理的变体，直接返回
	if thumbVariant == nil && webpVariant == nil && avifVariant == nil {
		converterLog.Debugf("No pending variants for %s, skip", image.Identifier)
		return
	}

	pool := worker.GetGlobalPool()
	if pool == nil {
		converterLog.Debugf("Worker pool not initialized for %s, skip conversion", image.Identifier)
		c.failPendingVariantsOnSubmitFailure(image, "worker pool not initialized", thumbVariant, webpVariant, avifVariant)
		return
	}

	// 获取图片对应的存储提供者
	storageProvider := c.getStorageForImage(image)
	if storageProvider == nil {
		converterLog.Debugf("No storage provider available for image %s (StorageConfigID=%d), skip conversion",
			image.Identifier, image.StorageConfigID)
		c.failPendingVariantsOnSubmitFailure(image, "storage provider unavailable", thumbVariant, webpVariant, avifVariant)
		return
	}

	// 提交统一流水线任务
	ok := pool.Submit(func() {
		task := &worker.ImagePipelineTask{
			ThumbVariantID:  getVariantID(thumbVariant),
			WebPVariantID:   getVariantID(webpVariant),
			AVIFVariantID:   getVariantID(avifVariant),
			ImageID:         image.ID,
			StoragePath:     image.StoragePath,
			ImageIdentifier: image.Identifier,
			FileSize:        image.FileSize,
			MimeType:        image.MimeType,
			Storage:         storageProvider,
			Settings:        settings,
			VariantRepo:     c.variantRepo,
			ImageRepo:       c.imageRepo,
			CacheHelper:     c.cacheHelper,
		}
		task.Execute()
	})

	if !ok {
		converterLog.Debugf("Failed to submit pipeline task for %s", image.Identifier)
		c.failPendingVariantsOnSubmitFailure(image, "worker task submission rejected", thumbVariant, webpVariant, avifVariant)
		return
	}

	if err := c.markImageProcessing(image); err != nil {
		converterLog.Debugf("Failed to update image status after submit: %v", err)
	}
}

func (c *Converter) failPendingVariantsOnSubmitFailure(image *models.Image, reason string, variants ...*models.ImageVariant) {
	hadPending := false
	for _, variant := range variants {
		if variant == nil || variant.Status != models.VariantStatusPending {
			continue
		}
		hadPending = true
		if err := c.variantRepo.UpdateFailed(variant.ID, reason); err != nil {
			converterLog.Debugf("Failed to mark variant %d failed after submit failure: %v", variant.ID, err)
		}
	}

	if !hadPending {
		return
	}

	if err := c.imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusFailed); err != nil {
		converterLog.Debugf("Failed to mark image %s failed after submit failure: %v", image.Identifier, err)
		return
	}
	image.VariantStatus = models.ImageVariantStatusFailed
}

func shouldTriggerVariantConversion(image *models.Image, settings *config.ImageProcessingSettings) bool {
	if image == nil || settings == nil {
		return false
	}

	thumbnailEnabled := settings.ThumbnailEnabled && len(settings.ThumbnailSizes) > 0
	webpEnabled := settings.IsFormatEnabled(models.FormatWebP)
	avifEnabled := settings.IsFormatEnabled(models.FormatAVIF) && vipsfile.SupportsAVIFEncoding()
	if !shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled) {
		return false
	}

	if image.MimeType == "image/gif" {
		return false
	}

	if settings.SkipSmallerThan > 0 {
		minSize := int64(settings.SkipSmallerThan * 1024)
		if image.FileSize < minSize {
			return false
		}
	}

	return true
}

func shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled bool) bool {
	return thumbnailEnabled || webpEnabled || avifEnabled
}

func variantReadyForSubmit(variant *models.ImageVariant, now time.Time) bool {
	if variant == nil || variant.Status != models.VariantStatusPending {
		return false
	}
	if variant.NextRetryAt != nil && variant.NextRetryAt.After(now) {
		return false
	}
	return true
}

// getVariantID 辅助函数：从变体指针获取ID
func getVariantID(v *models.ImageVariant) uint {
	if v == nil {
		return 0
	}
	return v.ID
}

func (c *Converter) markImageProcessing(image *models.Image) error {
	if image.VariantStatus == models.ImageVariantStatusProcessing {
		return nil
	}
	if err := c.imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusProcessing); err != nil {
		return err
	}
	image.VariantStatus = models.ImageVariantStatusProcessing
	return nil
}

// getStorageForImage 获取图片对应的存储提供者
func (c *Converter) getStorageForImage(image *models.Image) storage.Provider {
	// 如果图片指定了 StorageConfigID，尝试获取对应的 provider
	if image.StorageConfigID > 0 {
		provider, err := storage.GetByID(image.StorageConfigID)
		if err == nil {
			return provider
		}
		converterLog.Debugf("Failed to get storage provider ID=%d: %v",
			image.StorageConfigID, err)
		return nil
	}

	if c.storage != nil {
		return c.storage
	}

	provider := storage.GetDefault()
	if provider == nil {
		converterLog.Debugf("%v", fmt.Errorf("no default storage configured"))
	}
	return provider
}
