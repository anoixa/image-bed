package albums

import (
	"time"

	"github.com/anoixa/image-bed/cache"
	svcAlbums "github.com/anoixa/image-bed/internal/albums"
)

// Handler 相册处理器
type Handler struct {
	svc         *svcAlbums.Service
	cacheHelper *cache.Helper
	baseURL     string
}

// NewHandler 创建新的相册处理器
func NewHandler(svc *svcAlbums.Service, cacheProvider cache.Provider, baseURL string) *Handler {
	helperCfg := cache.HelperConfig{
		ImageCacheTTL:         cache.DefaultImageCacheExpiration,
		ImageDataCacheTTL:     1 * time.Hour,
		MaxCacheableImageSize: cache.DefaultMaxCacheableImageSize,
	}

	return &Handler{
		svc:         svc,
		cacheHelper: cache.NewHelper(cacheProvider, helperCfg),
		baseURL:     baseURL,
	}
}
