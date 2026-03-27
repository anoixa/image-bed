package image

import (
	"context"
	"fmt"

	"github.com/anoixa/image-bed/cache"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/vipsfile"
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
// 使用 PipelineTask 顺序生成缩略图、WebP 和 AVIF。
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

	thumbnailEnabled := settings.ThumbnailEnabled && len(settings.ThumbnailSizes) > 0
	webpEnabled := settings.IsFormatEnabled(models.FormatWebP)
	avifEnabled := settings.IsFormatEnabled(models.FormatAVIF) && vipsfile.SupportsAVIFEncoding()
	if !shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled) {
		utils.LogIfDevf("[Converter] All variant generation disabled, skipping")
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

	// 创建缩略图变体记录（如果启用）
	var thumbVariant *models.ImageVariant
	if thumbnailEnabled {
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
	if webpEnabled {
		webpVariant, err = c.variantRepo.UpsertPending(image.ID, models.FormatWebP)
		if err != nil {
			utils.LogIfDevf("[Converter] Failed to upsert WebP variant: %v", err)
			return
		}
		if webpVariant.Status != models.VariantStatusPending {
			utils.LogIfDevf("[Converter] WebP variant %d status=%s, skip", webpVariant.ID, webpVariant.Status)
			webpVariant = nil
		}
	}

	var avifVariant *models.ImageVariant
	if avifEnabled {
		avifVariant, err = c.variantRepo.UpsertPending(image.ID, models.FormatAVIF)
		if err != nil {
			utils.LogIfDevf("[Converter] Failed to upsert AVIF variant: %v", err)
			return
		}
		if avifVariant.Status != models.VariantStatusPending {
			utils.LogIfDevf("[Converter] AVIF variant %d status=%s, skip", avifVariant.ID, avifVariant.Status)
			avifVariant = nil
		}
	}

	// 如果没有需要处理的变体，直接返回
	if thumbVariant == nil && webpVariant == nil && avifVariant == nil {
		utils.LogIfDevf("[Converter] No pending variants for %s, skip", image.Identifier)
		return
	}

	pool := worker.GetGlobalPool()
	if pool == nil {
		utils.LogIfDevf("[Converter] Worker pool not initialized for %s, skip conversion", image.Identifier)
		return
	}

	// 获取图片对应的存储提供者
	storageProvider := c.getStorageForImage(image)
	if storageProvider == nil {
		utils.LogIfDevf("[Converter] No storage provider available for image %s (StorageConfigID=%d), skip conversion",
			image.Identifier, image.StorageConfigID)
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
		utils.LogIfDevf("[Converter] Failed to submit pipeline task for %s", image.Identifier)
		return
	}

	if err := c.markImageProcessing(image); err != nil {
		utils.LogIfDevf("[Converter] Failed to update image status after submit: %v", err)
	}
}

func shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled bool) bool {
	return thumbnailEnabled || webpEnabled || avifEnabled
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
		utils.LogIfDevf("[Converter] Failed to get storage provider ID=%d: %v",
			image.StorageConfigID, err)
		return nil
	}

	if c.storage != nil {
		return c.storage
	}

	provider := storage.GetDefault()
	if provider == nil {
		utils.LogIfDevf("[Converter] %v", fmt.Errorf("no default storage configured"))
	}
	return provider
}
