package images

import (
	"errors"
	"log"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DeleteRequestBody struct {
	ImageID []string `json:"identifier" binding:"required"`
}

// DeleteImagesHandler 批量删除图片
func DeleteImagesHandler(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	var requestBody DeleteRequestBody
	if err := context.ShouldBindJSON(&requestBody); err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid request body. 'identifier' field with a list of strings is required.")
		return
	}

	if len(requestBody.ImageID) == 0 {
		common.RespondError(context, http.StatusBadRequest, "No image identifiers provided for deletion.")
		return
	}

	affectedCount, err := images.DeleteImagesByIdentifiersAndUser(requestBody.ImageID, userID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to delete images due to an internal error.")
		return
	}

	// 清除缓存
	for _, imageID := range requestBody.ImageID {
		if err := cache.DeleteCachedImage(imageID); err != nil {
			log.Printf("P1 修复：Failed to delete cache for image %s: %v", imageID, err)
		}
		if err := cache.DeleteCachedImageData(imageID); err != nil {
			log.Printf("P1 修复：Failed to delete image data cache for image %s: %v", imageID, err)
		}
	}

	common.RespondSuccessMessage(context, "Delete request processed successfully.", gin.H{"deleted_count": affectedCount})
}

func DeleteSingleImageHandler(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	imageIdentifier := context.Param("identifier")
	if imageIdentifier == "" {
		common.RespondError(context, http.StatusBadRequest, "Image identifier is required.")
		return
	}

	err := images.DeleteImageByIdentifierAndUser(imageIdentifier, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(context, http.StatusNotFound, "Image not found or you do not have permission to delete it.")
			return
		}
		common.RespondError(context, http.StatusInternalServerError, "Failed to delete the image due to an internal error.")
		return
	}

	// 清除缓存
	if err := cache.DeleteCachedImage(imageIdentifier); err != nil {
		log.Printf("P1 修复：Failed to delete cache for image %s: %v", imageIdentifier, err)
	}
	if err := cache.DeleteCachedImageData(imageIdentifier); err != nil {
		log.Printf("P1 修复：Failed to delete image data cache for image %s: %v", imageIdentifier, err)
	}

	common.RespondSuccessMessage(context, "Image deleted successfully", nil)
}
