package images

import (
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/storage"
)

// Handler 图片处理器 - 使用依赖注入接收存储和缓存
type Handler struct {
	storageFactory *storage.Factory
	cacheHelper    *cache.Helper
}

// NewHandler 创建新的图片处理器
func NewHandler(storageFactory *storage.Factory, cacheFactory *cache.Factory) *Handler {
	return &Handler{
		storageFactory: storageFactory,
		cacheHelper:    cache.NewHelper(cacheFactory),
	}
}
