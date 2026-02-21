package images

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/gin-gonic/gin"
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

	ctx := c.Request.Context()
	result, err := h.imageService.DeleteBatch(ctx, requestBody.ImageID, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete images")
		return
	}

	common.RespondSuccessMessage(c, "Delete request processed successfully.", gin.H{"deleted_count": result.DeletedCount})
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

	ctx := c.Request.Context()
	result, err := h.imageService.DeleteSingle(ctx, imageIdentifier, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete the image")
		return
	}

	if !result.Success {
		if result.Error != nil {
			common.RespondError(c, http.StatusNotFound, result.Error.Error())
			return
		}
	}

	common.RespondSuccessMessage(c, "Image deleted successfully", nil)
}
