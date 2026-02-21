package images

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UpdateVisibilityRequest 更新图片可见性请求
type UpdateVisibilityRequest struct {
	IsPublic bool `json:"is_public" binding:"required"`
}

// UpdateImageVisibility 更新图片可见性
func (h *Handler) UpdateImageVisibility(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session")
		return
	}

	identifier := c.Param("identifier")
	if identifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required")
		return
	}

	var req UpdateVisibilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body. 'is_public' field is required.")
		return
	}

	// 获取图片信息
	image, err := h.repo.GetImageByIdentifier(identifier)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			common.RespondError(c, http.StatusNotFound, "Image not found")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get image information")
		return
	}

	// 检查权限（只能修改自己的图片）
	if image.UserID != userID {
		common.RespondError(c, http.StatusForbidden, "You don't have permission to update this image")
		return
	}

	// 更新可见性
	updates := map[string]interface{}{
		"is_public": req.IsPublic,
	}
	updatedImage, err := h.repo.UpdateImageByIdentifier(identifier, updates)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to update image visibility")
		return
	}

	// 更新缓存
	ctx := c.Request.Context()
	_ = h.cacheHelper.CacheImage(ctx, updatedImage) // 缓存更新失败不影响主流程

	visibility := "private"
	if updatedImage.IsPublic {
		visibility = "public"
	}

	common.RespondSuccessMessage(c, "Image visibility updated successfully", gin.H{
		"identifier":   updatedImage.Identifier,
		"is_public":    updatedImage.IsPublic,
		"visibility":   visibility,
		"url":          utils.BuildImageURL(updatedImage.Identifier),
	})
}
