package images

import (
	"errors"
	"log"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DeleteRequestBody struct {
	ImageID []string `json:"identifier" binding:"required"`
}

// DeleteImages 批量删除图片
func (h *Handler) DeleteImages(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session")
		return
	}

	var requestBody DeleteRequestBody
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body. 'identifier' field with a list of strings is required.")
		return
	}

	if len(requestBody.ImageID) == 0 {
		common.RespondError(c, http.StatusBadRequest, "No image identifiers provided for deletion.")
		return
	}

	affectedCount, err := h.repo.DeleteImagesByIdentifiersAndUser(requestBody.ImageID, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete images due to an internal error.")
		return
	}

	// 清除缓存
	ctx := c.Request.Context()
	for _, imageID := range requestBody.ImageID {
		if err := h.cacheHelper.DeleteCachedImage(ctx, imageID); err != nil {
			log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(imageID), err)
		}
		if err := h.cacheHelper.DeleteCachedImageData(ctx, imageID); err != nil {
			log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(imageID), err)
		}
	}

	common.RespondSuccessMessage(c, "Delete request processed successfully.", gin.H{"deleted_count": affectedCount})
}

// DeleteSingleImage 删除单张图片
func (h *Handler) DeleteSingleImage(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session")
		return
	}

	imageIdentifier := c.Param("identifier")
	if imageIdentifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required.")
		return
	}

	err := h.repo.DeleteImageByIdentifierAndUser(imageIdentifier, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Image not found or you do not have permission to delete it.")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete the image due to an internal error.")
		return
	}

	// 清除缓存
	ctx := c.Request.Context()
	if err := h.cacheHelper.DeleteCachedImage(ctx, imageIdentifier); err != nil {
		log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(imageIdentifier), err)
	}
	if err := h.cacheHelper.DeleteCachedImageData(ctx, imageIdentifier); err != nil {
		log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(imageIdentifier), err)
	}

	common.RespondSuccessMessage(c, "Image deleted successfully", nil)
}
