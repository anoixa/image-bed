package albums

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AddImagesToAlbumRequest 添加图片到相册请求
type AddImagesToAlbumRequest struct {
	ImageIDs []uint `json:"image_ids" binding:"required,min=1"`
}

// AlbumImageHandler 相册图片处理器
type AlbumImageHandler struct {
	repo        *albums.Repository
	imageRepo   *images.Repository
	cacheHelper *cache.Helper
}

// NewAlbumImageHandler 创建相册图片处理器
func NewAlbumImageHandler(albumsRepo *albums.Repository, imagesRepo *images.Repository, cacheProvider cache.Provider) *AlbumImageHandler {
	return &AlbumImageHandler{
		repo:        albumsRepo,
		imageRepo:   imagesRepo,
		cacheHelper: cache.NewHelper(cacheProvider),
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

	// 验证相册存在且属于当前用户
	_, err = h.repo.GetAlbumWithImagesByID(uint(albumID), userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			common.RespondError(c, http.StatusNotFound, "Album not found or access denied")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get album")
		return
	}

	// 批量添加图片
	var addedCount int
	var failedIDs []uint

	for _, imageID := range req.ImageIDs {
		// 验证图片存在且属于当前用户
		image, err := h.imageRepo.GetImageByIDAndUser(imageID, userID)
		if err != nil {
			failedIDs = append(failedIDs, imageID)
			continue
		}

		// 添加到相册
		if err := h.repo.AddImageToAlbum(uint(albumID), userID, image); err != nil {
			failedIDs = append(failedIDs, imageID)
			continue
		}
		addedCount++
	}

	common.RespondSuccess(c, gin.H{
		"album_id":    albumID,
		"added_count": addedCount,
		"failed_ids":  failedIDs,
	})

	// 如果成功添加了图片，清除相关缓存
	if addedCount > 0 {
		go func() {
			ctx := context.Background()
			if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
				log.Printf("Failed to delete album cache for %d: %v", albumID, err)
			}
			if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
				log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
			}
		}()
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

	// 获取图片 ID
	imageIDStr := c.Param("imageId")
	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid image ID format")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	// 获取图片
	image, err := h.imageRepo.GetImageByIDAndUser(uint(imageID), userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			common.RespondError(c, http.StatusNotFound, "Image not found")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get image")
		return
	}

	// 从相册移除图片
	if err := h.repo.RemoveImageFromAlbum(uint(albumID), userID, image); err != nil {
		if err == gorm.ErrRecordNotFound {
			common.RespondError(c, http.StatusNotFound, "Album not found or access denied")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to remove image from album")
		return
	}

	common.RespondSuccessMessage(c, "Image removed from album successfully", nil)

	// 清除相关缓存
	go func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
			log.Printf("Failed to delete album cache for %d: %v", albumID, err)
		}
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	}()
}
