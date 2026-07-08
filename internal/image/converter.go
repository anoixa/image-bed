package image

import (
	"errors"
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
	"gorm.io/gorm"
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
	c.triggerConversion(image, false, nil)
}

// TriggerConversionWithLocalFile triggers conversion with a pre-staged local file
// lease, allowing the pipeline to skip downloading from remote storage.
func (c *Converter) TriggerConversionWithLocalFile(image *models.Image, localFile *worker.LocalFileLease) {
	c.triggerConversion(image, false, localFile)
}

// TriggerConversionFromSweeper re-submits work selected by the sweeper while
// preserving each variant's retry window.
func (c *Converter) TriggerConversionFromSweeper(image *models.Image) {
	c.triggerConversion(image, false, nil)
}

func (c *Converter) triggerConversion(image *models.Image, ignoreRetryWindow bool, localFile *worker.LocalFileLease) {
	// Register cleanup FIRST so transferred temp files are removed on any exit
	// path before the pipeline task takes ownership.
	submitted := false
	defer func() {
		if !submitted && localFile != nil {
			localFile.CleanupTransferred()
		}
	}()

	ctx, cancel := utils.DetachedContext(5 * time.Second)
	defer cancel()
	now := time.Now()
	variantRepo := c.variantRepo.WithContext(ctx)
	imageRepo := c.imageRepo.WithContext(ctx)

	settings, err := c.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		converterLog.Warnf("Failed to load image processing settings for %s: %v", image.Identifier, err)
		return
	}

	if !shouldTriggerVariantConversion(image, settings) {
		return
	}

	thumbnailEnabled := settings.ThumbnailEnabled && len(settings.ThumbnailSizes) > 0
	webpEnabled := settings.IsFormatEnabled(models.FormatWebP)
	avifEnabled := settings.IsFormatEnabled(models.FormatAVIF) && vipsfile.SupportsAVIFEncoding()

	// 先解析存储可写性（单一快照），分三态：
	//   未加载 → 沿用旧行为（不建行 + markUnavailable）
	//   已加载但禁用 → 建行后暂停（设退避，不提交、不增重试、不标 failed），待存储重新启用后由 sweeper 恢复
	//   可写 → 建行后提交
	writableState, resolved := c.resolveStorageWritable(image)
	if writableState == storageWriteUnavailable {
		converterLog.Warnf("Storage provider unavailable for image %s (StorageConfigID=%d)",
			image.Identifier, image.StorageConfigID)
		c.markImageConversionUnavailable(imageRepo, image, "storage provider unavailable")
		return
	}

	// 创建缩略图变体记录（如果启用）
	var thumbVariant *models.ImageVariant
	if thumbnailEnabled {
		size := settings.ThumbnailSizes[0]
		thumbFormat := models.FormatThumbnailSize(size.Width)
		thumbVariant, err = prepareVariantForSubmit(variantRepo, image.ID, thumbFormat, now, ignoreRetryWindow)
		if err != nil {
			converterLog.Warnf("Failed to prepare thumbnail variant for image %s: %v", image.Identifier, err)
			return
		}
	}

	// 创建 WebP 变体记录（如果启用）
	var webpVariant *models.ImageVariant
	if webpEnabled {
		webpVariant, err = prepareVariantForSubmit(variantRepo, image.ID, models.FormatWebP, now, ignoreRetryWindow)
		if err != nil {
			converterLog.Warnf("Failed to prepare WebP variant for image %s: %v", image.Identifier, err)
			c.failPendingVariantsOnSubmitFailure(imageRepo, variantRepo, image, fmt.Sprintf("submit aborted during webp preparation: %v", err), thumbVariant)
			return
		}
	}

	var avifVariant *models.ImageVariant
	if avifEnabled {
		avifVariant, err = prepareVariantForSubmit(variantRepo, image.ID, models.FormatAVIF, now, ignoreRetryWindow)
		if err != nil {
			converterLog.Warnf("Failed to prepare AVIF variant for image %s: %v", image.Identifier, err)
			c.failPendingVariantsOnSubmitFailure(imageRepo, variantRepo, image, fmt.Sprintf("submit aborted during avif preparation: %v", err), thumbVariant, webpVariant)
			return
		}
	}

	// 如果没有需要处理的变体，直接返回
	if thumbVariant == nil && webpVariant == nil && avifVariant == nil {
		return
	}

	// 已加载但禁用：暂停。保留 pending 行，设退避，不提交、不增重试、不标 failed。
	if writableState == storageWriteDisabled {
		backoffAt := now.Add(conversionPauseBackoff)
		if err := variantRepo.SetPendingBackoff(collectVariantIDs(thumbVariant, webpVariant, avifVariant), backoffAt); err != nil {
			converterLog.Warnf("Failed to set pause backoff for image %s: %v", image.Identifier, err)
		}
		converterLog.Infof("Paused variant conversion for image %s: storage disabled (backoff until %s)",
			image.Identifier, backoffAt.Format(time.RFC3339))
		return
	}

	// 可写：检查 worker pool 后提交。
	pool := worker.GetGlobalPool()
	if pool == nil {
		converterLog.Warnf("Worker pool unavailable for image %s", image.Identifier)
		c.markImageConversionUnavailable(imageRepo, image, "worker pool not initialized")
		return
	}

	storageProvider := resolved.provider
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
			StorageConfigID: image.StorageConfigID,
			Settings:        settings,
			VariantRepo:     c.variantRepo,
			ImageRepo:       c.imageRepo,
			CacheHelper:     c.cacheHelper,
			LocalFile:       localFile,
		}
		task.Execute()
	})

	if !ok {
		converterLog.Warnf("Failed to submit pipeline task for %s", image.Identifier)
		c.failPendingVariantsOnSubmitFailure(imageRepo, variantRepo, image, "worker task submission rejected", thumbVariant, webpVariant, avifVariant)
		return
	}
	submitted = true

	if err := c.markImageProcessing(imageRepo, image); err != nil {
		converterLog.Warnf("Failed to update image %s status after submit: %v", image.Identifier, err)
	}
}

func (c *Converter) markImageConversionUnavailable(imageRepo *images.Repository, image *models.Image, reason string) {
	if image == nil || imageRepo == nil {
		return
	}

	if err := imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusFailed); err != nil {
		converterLog.Warnf("Failed to mark image %s failed after conversion unavailable (%s): %v", image.Identifier, reason, err)
		return
	}
	image.VariantStatus = models.ImageVariantStatusFailed
	if c.cacheHelper != nil {
		ctx, cancel := utils.DetachedContext(5 * time.Second)
		defer cancel()
		_ = c.cacheHelper.DeleteCachedImage(ctx, image.Identifier)
		_ = c.cacheHelper.DeleteCachedImageVariants(ctx, image.ID)
	}
}

func (c *Converter) failPendingVariantsOnSubmitFailure(imageRepo *images.Repository, variantRepo *images.VariantRepository, image *models.Image, reason string, variants ...*models.ImageVariant) {
	hadPending := false
	for _, variant := range variants {
		if variant == nil || variant.Status != models.VariantStatusPending {
			continue
		}
		hadPending = true
		if err := variantRepo.ForceUpdateFailed(variant.ID, reason); err != nil {
			converterLog.Warnf("Failed to mark variant %d failed after submit failure: %v", variant.ID, err)
		}
	}

	if !hadPending {
		return
	}

	if err := imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusFailed); err != nil {
		converterLog.Warnf("Failed to mark image %s failed after submit failure: %v", image.Identifier, err)
		return
	}
	image.VariantStatus = models.ImageVariantStatusFailed
	if c.cacheHelper != nil {
		ctx, cancel := utils.DetachedContext(5 * time.Second)
		defer cancel()
		_ = c.cacheHelper.DeleteCachedImage(ctx, image.Identifier)
		_ = c.cacheHelper.DeleteCachedImageVariants(ctx, image.ID)
	}
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

func prepareVariantForSubmit(variantRepo *images.VariantRepository, imageID uint, format string, now time.Time, ignoreRetryWindow bool) (*models.ImageVariant, error) {
	variant, err := variantRepo.GetVariantByImageIDAndFormat(imageID, format)
	if err == nil {
		if variant.Status == models.VariantStatusCanceled {
			variant, err = variantRepo.ResetCanceledToPending(variant.ID)
			if err != nil {
				return nil, err
			}
		}
		if variantReadyForSubmit(variant, now, ignoreRetryWindow) {
			return variant, nil
		}
		return nil, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	variant, err = variantRepo.UpsertPending(imageID, format)
	if err != nil {
		return nil, err
	}
	if !variantReadyForSubmit(variant, now, ignoreRetryWindow) {
		return nil, nil
	}
	return variant, nil
}

func shouldStartVariantPipeline(thumbnailEnabled, webpEnabled, avifEnabled bool) bool {
	return thumbnailEnabled || webpEnabled || avifEnabled
}

func variantReadyForSubmit(variant *models.ImageVariant, now time.Time, ignoreRetryWindow bool) bool {
	if variant == nil || variant.Status != models.VariantStatusPending {
		return false
	}
	if !ignoreRetryWindow && variant.NextRetryAt != nil && variant.NextRetryAt.After(now) {
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

func (c *Converter) markImageProcessing(imageRepo *images.Repository, image *models.Image) error {
	if image.VariantStatus == models.ImageVariantStatusProcessing {
		return nil
	}
	if err := imageRepo.UpdateVariantStatus(image.ID, models.ImageVariantStatusProcessing); err != nil {
		return err
	}
	image.VariantStatus = models.ImageVariantStatusProcessing
	return nil
}

// conversionPauseBackoff 在图片存储被禁用时应用到 pending 变体上的退避时长，
// 使 sweeper 不会每个周期都重投；>= sweeper 周期（5 分钟）。
const conversionPauseBackoff = 10 * time.Minute

type storageWritableState int

const (
	storageWriteOK storageWritableState = iota
	storageWriteDisabled
	storageWriteUnavailable
)

type resolvedStorage struct {
	provider storage.Provider
}

// resolveStorageWritable 从单一 registry 快照把图片存储解析为三态之一。
func (c *Converter) resolveStorageWritable(image *models.Image) (storageWritableState, resolvedStorage) {
	provider, _, err := storage.ResolveWritable(image.StorageConfigID)
	switch {
	case err == nil:
		return storageWriteOK, resolvedStorage{provider: provider}
	case errors.Is(err, storage.ErrProviderDisabled):
		return storageWriteDisabled, resolvedStorage{}
	default:
		return storageWriteUnavailable, resolvedStorage{}
	}
}

// collectVariantIDs 收集非空变体的 ID。
func collectVariantIDs(variants ...*models.ImageVariant) []uint {
	ids := make([]uint, 0, len(variants))
	for _, v := range variants {
		if v != nil {
			ids = append(ids, v.ID)
		}
	}
	return ids
}
