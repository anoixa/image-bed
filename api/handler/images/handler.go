package images

import (
	"context"

	"github.com/anoixa/image-bed/cache"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"gorm.io/gorm"
)

// Handler 图片处理器
type Handler struct {
	cacheHelper      *cache.Helper
	repo             *images.Repository
	converter        *image.Converter
	configManager    *configSvc.Manager
	variantService   *image.VariantService
	thumbnailService *image.ThumbnailService
	variantRepo      *images.VariantRepository
	imageService     *image.Service
}

// NewHandler 图片处理器
func NewHandler(cacheProvider cache.Provider, imagesRepo *images.Repository, db *gorm.DB, converter *image.Converter, configManager *configSvc.Manager) *Handler {
	// 创建变体仓库和服务
	variantRepo := images.NewVariantRepository(db)
	variantService := image.NewVariantService(variantRepo, configManager, converter)

	// 创建缩略图服务
	thumbnailService := image.NewThumbnailService(variantRepo, configManager, storage.GetDefault(), converter)

	// 创建图片服务
	cacheHelper := cache.NewHelper(cacheProvider)
	imageService := image.NewService(imagesRepo, variantRepo, converter, thumbnailService, variantService, cacheHelper)

	return &Handler{
		cacheHelper:      cacheHelper,
		repo:             imagesRepo,
		converter:        converter,
		configManager:    configManager,
		variantRepo:      variantRepo,
		variantService:   variantService,
		thumbnailService: thumbnailService,
		imageService:     imageService,
	}
}

// getStorageConfigID 根据存储名称获取存储配置ID
func (h *Handler) getStorageConfigID(c interface{ Query(string) string }, storageName string) (uint, error) {
	// 如果 storageName 为空，回退默认存储的ID
	return storage.GetDefaultID(), nil
}

// warmCache 预热图片缓存
func (h *Handler) warmCache(image *models.Image) {
	if h.cacheHelper == nil {
		return
	}
	ctx := context.Background()
	_ = h.cacheHelper.CacheImage(ctx, image)
}
