package albums

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/repo/images"
	svcAlbums "github.com/anoixa/image-bed/internal/albums"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AddImagesToAlbumRequest 添加图片到相册请求
type AddImagesToAlbumRequest struct {
	ImageIDs []uint `json:"image_ids" binding:"required,min=1"`
}

// AlbumImageHandler 相册图片处理器
type AlbumImageHandler struct {
	svc         *svcAlbums.Service
	imageRepo   *images.Repository
	cacheHelper *cache.Helper
}

// NewAlbumImageHandler 创建相册图片处理器
func NewAlbumImageHandler(albumsSvc *svcAlbums.Service, imagesRepo *images.Repository, cacheProvider cache.Provider, cfg *config.Config) *AlbumImageHandler {
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

	return &AlbumImageHandler{
		svc:         albumsSvc,
		imageRepo:   imagesRepo,
		cacheHelper: cache.NewHelper(cacheProvider, helperCfg),
	}
}

// AddImagesToAlbumHandler 添加图片到相册
func (h *AlbumImageHandler) AddImagesToAlbumHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	var req AddImagesToAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	_, err = h.svc.GetAlbumWithImagesByID(uint(albumID), userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			common.RespondError(c, http.StatusNotFound, "Album not found or access denied")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get album")
		return
	}

	// 批量查询图片
	imgs, err := h.imageRepo.GetImagesByIDsAndUser(req.ImageIDs, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get imgs")
		return
	}

	foundIDs := make(map[uint]bool)
	imageIDsToAdd := make([]uint, 0, len(imgs))
	for _, img := range imgs {
		foundIDs[img.ID] = true
		imageIDsToAdd = append(imageIDsToAdd, img.ID)
	}

	var failedIDs []uint
	for _, id := range req.ImageIDs {
		if !foundIDs[id] {
			failedIDs = append(failedIDs, id)
		}
	}

	// 批量添加到相册
	addedCount := 0
	if len(imageIDsToAdd) > 0 {
		if err := h.svc.AddImagesToAlbum(uint(albumID), userID, imageIDsToAdd); err != nil {
			common.RespondError(c, http.StatusInternalServerError, "Failed to add imgs to album")
			return
		}
		addedCount = len(imageIDsToAdd)
	}

	common.RespondSuccess(c, gin.H{
		"album_id":    albumID,
		"added_count": addedCount,
		"failed_ids":  failedIDs,
	})

	if addedCount > 0 {
		utils.SafeGo(func() {
			ctx := context.Background()
			if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
				log.Printf("Failed to delete album cache for %d: %v", albumID, err)
			}
			if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
				log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
			}
		})
	}
}

// RemoveImageFromAlbumHandler 从相册移除图片
func (h *AlbumImageHandler) RemoveImageFromAlbumHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	imageIDStr := c.Param("imageId")
	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid image ID format")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	image, err := h.imageRepo.GetImageByIDAndUser(uint(imageID), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Image not found")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get image")
		return
	}

	// 从相册移除图片
	if err := h.svc.RemoveImageFromAlbum(uint(albumID), userID, image); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Album not found or access denied")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to remove image from album")
		return
	}

	common.RespondSuccessMessage(c, "Image removed from album successfully", nil)

	// 清除相关缓存
	utils.SafeGo(func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
			log.Printf("Failed to delete album cache for %d: %v", albumID, err)
		}
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	})
}
