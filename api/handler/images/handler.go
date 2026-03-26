package images

import (
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/internal/random"
	"gorm.io/gorm"
)

type Handler struct {
	cacheHelper      *cache.Helper
	imageDataCaching bool
	repo             *images.Repository
	configManager    *configSvc.Manager
	variantService   *image.VariantService
	thumbnailService *image.ThumbnailService
	variantRepo      *images.VariantRepository
	writeService     *image.WriteService
	readService      *image.ReadService
	deleteService    *image.DeleteService
	queryService     *image.QueryService
	randomService    *random.Service
	baseURL          string
}

func NewHandler(cacheProvider cache.Provider, imagesRepo *images.Repository, db *gorm.DB, converter *image.Converter, configManager *configSvc.Manager, cfg *config.Config, baseURL string, albumsRepo *albums.Repository) *Handler {
	variantRepo := images.NewVariantRepository(db)
	variantService := image.NewVariantService(variantRepo, configManager)

	thumbnailService := image.NewThumbnailService(variantRepo)

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

	cacheHelper := cache.NewHelper(cacheProvider, helperCfg)
	writeService := image.NewWriteService(imagesRepo, albumsRepo, converter, cacheHelper, baseURL)
	readService := image.NewReadService(imagesRepo, variantService, converter, cacheHelper, baseURL, image.SubmitBackgroundTask)
	deleteService := image.NewDeleteService(imagesRepo, variantRepo, cacheHelper)
	queryService := image.NewQueryService(imagesRepo, configManager)
	var randomService *random.Service
	if configManager != nil {
		randomService = random.NewService(configManager)
	}

	return &Handler{
		cacheHelper:      cacheHelper,
		imageDataCaching: cfg != nil && cfg.CacheEnableImageCaching,
		repo:             imagesRepo,
		configManager:    configManager,
		variantRepo:      variantRepo,
		variantService:   variantService,
		thumbnailService: thumbnailService,
		writeService:     writeService,
		readService:      readService,
		deleteService:    deleteService,
		queryService:     queryService,
		randomService:    randomService,
		baseURL:          baseURL,
	}
}
