package albums

import (
	"context"
	"log"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/gin-gonic/gin"
)

type createAlbumRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Description string `json:"description" binding:"max=255"`
}

func (h *Handler) CreateAlbumHandler(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	var req createAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	album := models.Album{
		Name:        req.Name,
		Description: req.Description,
		UserID:      userID,
	}

	if err := h.repo.CreateAlbum(&album); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create albums.")
		return
	}

	// 清除用户相册列表缓存
	go func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	}()

	common.RespondSuccess(c, album)
}
