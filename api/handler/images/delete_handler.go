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
// @Summary      Delete multiple images
// @Description  Delete multiple images by their identifiers in a single request
// @Tags         images
// @Accept       json
// @Produce      json
// @Param        request  body      DeleteRequestBody  true  "List of image identifiers to delete"
// @Success      200      {object}  common.Response  "Delete request processed successfully"
// @Failure      400      {object}  common.Response  "Invalid request body"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /images/delete [post]
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
// @Summary      Delete single image
// @Description  Delete a single image by its identifier
// @Tags         images
// @Accept       json
// @Produce      json
// @Param        identifier  path      string  true   "Image identifier"
// @Success      200         {object}  common.Response  "Image deleted successfully"
// @Failure      400         {object}  common.Response  "Invalid identifier"
// @Failure      401         {object}  common.Response  "Unauthorized"
// @Failure      404         {object}  common.Response  "Image not found"
// @Failure      500         {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /images/{identifier} [delete]
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
