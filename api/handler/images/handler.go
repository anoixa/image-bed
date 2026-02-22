package images

import (
	"context"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"gorm.io/gorm"
)

// Handler 图片处理器
type Handler struct {
	cacheHelper           *cache.Helper
	repo                  *images.Repository
	converter             *image.Converter
	configManager         *configSvc.Manager
	variantService        *image.VariantService
	thumbnailService      *image.ThumbnailService
	variantRepo           *images.VariantRepository
	imageService          *image.Service
	baseURL               string
	uploadMaxBatchTotalMB int
}

// NewHandler 图片处理器
func NewHandler(cacheProvider cache.Provider, imagesRepo *images.Repository, db *gorm.DB, converter *image.Converter, configManager *configSvc.Manager, cfg *config.Config, baseURL string, uploadMaxBatchTotalMB int) *Handler {
	// 创建变体仓库和服务
	variantRepo := images.NewVariantRepository(db)
	variantService := image.NewVariantService(variantRepo, configManager, converter)

	// 创建缩略图服务
	imageRepo := images.NewRepository(db)
	thumbnailService := image.NewThumbnailService(variantRepo, imageRepo, configManager, storage.GetDefault(), converter)

	helperCfg := cache.HelperConfig{
		ImageCacheTTL:         cache.DefaultImageCacheExpiration,
		ImageDataCacheTTL:     1 * time.Hour,
		MaxCacheableImageSize: cache.DefaultMaxCacheableImageSize,
	}
	if cfg != nil {
		if cfg.CacheImageCacheTTL > 0 {
			helperCfg.ImageCacheTTL = time.Duration(cfg.CacheImageCacheTTL) * time.Second
		}
		if cfg.CacheImageDataCacheTTL > 0 {
			helperCfg.ImageDataCacheTTL = time.Duration(cfg.CacheImageDataCacheTTL) * time.Second
		}
		if cfg.CacheMaxCacheableImageSize > 0 {
			helperCfg.MaxCacheableImageSize = cfg.CacheMaxCacheableImageSize
		}
	}

	// 创建图片服务
	cacheHelper := cache.NewHelper(cacheProvider, helperCfg)
	imageService := image.NewService(imagesRepo, variantRepo, converter, thumbnailService, variantService, cacheHelper, baseURL)

	return &Handler{
		cacheHelper:           cacheHelper,
		repo:                  imagesRepo,
		converter:             converter,
		configManager:         configManager,
		variantRepo:           variantRepo,
		variantService:        variantService,
		thumbnailService:      thumbnailService,
		imageService:          imageService,
		baseURL:               baseURL,
		uploadMaxBatchTotalMB: uploadMaxBatchTotalMB,
	}
}


// warmCache 预热图片缓存
func (h *Handler) warmCache(image *models.Image) {
	if h.cacheHelper == nil {
		return
	}
	ctx := context.Background()
	_ = h.cacheHelper.CacheImage(ctx, image)
}
