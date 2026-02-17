package images

import (
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/repositories"
	configSvc "github.com/anoixa/image-bed/internal/services/config"
	"github.com/anoixa/image-bed/internal/services/image"
	"github.com/anoixa/image-bed/storage"
)

// Handler 图片处理器 - 使用依赖注入接收存储和缓存
type Handler struct {
	storageFactory *storage.Factory
	cacheHelper    *cache.Helper
	repo           *images.Repository
	converter      *image.Converter
	configManager  *configSvc.Manager
	variantService *image.VariantService
	variantRepo    images.VariantRepository
}

// NewHandler 图片处理器
func NewHandler(storageFactory *storage.Factory, cacheFactory *cache.Factory, repos *repositories.Repositories, converter *image.Converter, configManager *configSvc.Manager) *Handler {
	// 创建变体仓库和服务
	variantRepo := images.NewVariantRepository(repos.DB())
	variantService := image.NewVariantService(variantRepo, configManager, converter)

	return &Handler{
		storageFactory: storageFactory,
		cacheHelper:    cache.NewHelper(cacheFactory),
		repo:           repos.Images,
		converter:      converter,
		configManager:  configManager,
		variantRepo:    variantRepo,
		variantService: variantService,
	}
}

// getStorageConfigID 根据存储名称获取存储配置ID
// 如果 storageName 为空，则返回默认存储的ID
func (h *Handler) getStorageConfigID(c interface{ Query(string) string }, storageName string) (uint, error) {
	if storageName != "" {
		return h.storageFactory.GetIDByName(storageName)
	}
	return h.storageFactory.GetDefaultID(), nil
}
