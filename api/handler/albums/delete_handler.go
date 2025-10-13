package albums

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/gin-gonic/gin"
)

func DeleteAlbumHandler(context *gin.Context) {
	// 获取相册 ID
	albumIDStr := context.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	userID := context.GetUint(middleware.ContextUserIDKey)

	err = albums.DeleteAlbum(uint(albumID), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found or access denied") {
			common.RespondError(context, http.StatusNotFound, err.Error())
		} else {
			log.Printf("Error deleting album %d for user %d: %v", albumID, userID, err)
			common.RespondError(context, http.StatusInternalServerError, "Failed to delete album due to an internal error")
		}
		return
	}

	common.RespondSuccessMessage(context, "Image deleted successfully", nil)
}
