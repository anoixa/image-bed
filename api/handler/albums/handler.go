package albums

import (
	"time"

	"github.com/anoixa/image-bed/cache"
	svcAlbums "github.com/anoixa/image-bed/internal/albums"
	"github.com/anoixa/image-bed/utils"
)

type Handler struct {
	svc         *svcAlbums.Service
	cacheHelper *cache.Helper
	baseURL     string
}

var albumAsync = utils.SafeGo

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
