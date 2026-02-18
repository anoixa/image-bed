package albums

import (
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/albums"
)

// Handler 相册处理器
type Handler struct {
	repo        *albums.Repository
	cacheHelper *cache.Helper
}

// NewHandler 创建新的相册处理器
func NewHandler(repo *albums.Repository, cacheFactory *cache.Factory) *Handler {
	return &Handler{
		repo:        repo,
		cacheHelper: cache.NewHelper(cacheFactory),
	}
}
