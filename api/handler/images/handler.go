package images

import (
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/repositories"
	"github.com/anoixa/image-bed/storage"
)

// Handler 图片处理器 - 使用依赖注入接收存储和缓存
type Handler struct {
	storageFactory *storage.Factory
	cacheHelper    *cache.Helper
	repo           *images.Repository
}

// NewHandler 创建新的图片处理器
func NewHandler(storageFactory *storage.Factory, cacheFactory *cache.Factory, repos *repositories.Repositories) *Handler {
	return &Handler{
		storageFactory: storageFactory,
		cacheHelper:    cache.NewHelper(cacheFactory),
		repo:           repos.Images,
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
