package albums

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UpdateAlbumRequest 更新相册请求
type UpdateAlbumRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Description string `json:"description" binding:"max=255"`
}

// UpdateAlbumResponse 更新相册响应
type UpdateAlbumResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UpdatedAt   int64  `json:"updated_at"`
}

// UpdateAlbumHandler 更新相册
func (h *Handler) UpdateAlbumHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	var req UpdateAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	album, err := h.svc.GetAlbumWithImagesByID(uint(albumID), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Album not found or access denied")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get album")
		return
	}

	album.Name = req.Name
	album.Description = req.Description

	if err := h.svc.UpdateAlbum(album); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to update album")
		return
	}

	// 清除相册缓存和用户的相册列表缓存
	utils.SafeGo(func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
			log.Printf("Failed to delete album cache for %d: %v", albumID, err)
		}
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	})

	common.RespondSuccess(c, UpdateAlbumResponse{
		ID:          album.ID,
		Name:        album.Name,
		Description: album.Description,
		UpdatedAt:   album.UpdatedAt.Unix(),
	})
}
