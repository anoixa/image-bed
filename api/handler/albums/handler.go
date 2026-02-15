package albums

import (
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/internal/repositories"
)

// Handler 相册处理器
type Handler struct {
	repo        *albums.Repository
	cacheHelper *cache.Helper
}

// NewHandler 创建新的相册处理器
func NewHandler(repos *repositories.Repositories, cacheFactory *cache.Factory) *Handler {
	return &Handler{
		repo:        repos.Albums,
		cacheHelper: cache.NewHelper(cacheFactory),
	}
}
