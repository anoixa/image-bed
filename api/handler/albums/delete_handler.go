package albums

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

// DeleteAlbumHandler 删除相册
// @Summary      Delete album
// @Description  Delete an album by ID (images in the album will not be deleted)
// @Tags         albums
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Album ID"
// @Success      200  {object}  common.Response  "Album deleted successfully"
// @Failure      400  {object}  common.Response  "Invalid album ID format"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      403  {object}  common.Response  "Permission denied"
// @Failure      404  {object}  common.Response  "Album not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /albums/{id} [delete]
func (h *Handler) DeleteAlbumHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	err = h.svc.DeleteAlbum(uint(albumID), userID)
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
	utils.SafeGo(func() {
		ctx := c.Copy().Request.Context()
		if err := h.cacheHelper.DeleteCachedAlbum(ctx, uint(albumID)); err != nil {
			log.Printf("Failed to delete album cache for %d: %v", albumID, err)
		}
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	})

	common.RespondSuccessMessage(c, "Album deleted successfully", nil)
}
