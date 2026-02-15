package albums

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/gin-gonic/gin"
)

func (h *Handler) DeleteAlbumHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	err = h.repo.DeleteAlbum(uint(albumID), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found or access denied") {
			common.RespondError(c, http.StatusNotFound, err.Error())
		} else {
			log.Printf("Error deleting album %d for user %d: %v", albumID, userID, err)
			common.RespondError(c, http.StatusInternalServerError, "Failed to delete album due to an internal error")
		}
		return
	}

	// 清除相册缓存和用户的相册列表缓存
	go func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
			log.Printf("Failed to delete album cache for %d: %v", albumID, err)
		}
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	}()

	common.RespondSuccessMessage(c, "Album deleted successfully", nil)
}
