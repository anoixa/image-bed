package admin

import (
	"fmt"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/gin-gonic/gin"
)

// ConversionHandler 转换配置处理器
type ConversionHandler struct {
	configManager *config.Manager
}

// NewConversionHandler 创建处理器
func NewConversionHandler(cm *config.Manager) *ConversionHandler {
	return &ConversionHandler{configManager: cm}
}

// GetConfig 获取转换配置
// @Summary      Get image processing configuration
// @Description  Get current image processing settings (thumbnail, WebP conversion, etc.)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Image processing settings"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/conversion [get]
func (h *ConversionHandler) GetConfig(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		return
	}

	common.RespondSuccess(c, settings)
}

// UpdateConfigRequest 更新配置请求（所有字段可选）
type UpdateConfigRequest struct {
	ThumbnailEnabled         *bool                  `json:"thumbnail_enabled,omitempty"`
	ThumbnailSizes           []models.ThumbnailSize `json:"thumbnail_sizes,omitempty"`
	ThumbnailQuality         *int                   `json:"thumbnail_quality,omitempty"`
	ConversionEnabledFormats []string               `json:"conversion_enabled_formats,omitempty"`
	WebPQuality              *int                   `json:"webp_quality,omitempty"`
	WebPEffort               *int                   `json:"webp_effort,omitempty"`
	AVIFQuality              *int                   `json:"avif_quality,omitempty"`
	AVIFSpeed                *int                   `json:"avif_speed,omitempty"`
	AVIFExperimental         *bool                  `json:"avif_experimental,omitempty"`
	SkipSmallerThan          *int                   `json:"skip_smaller_than,omitempty"`
	MaxDimension             *int                   `json:"max_dimension,omitempty"`
	DefaultAlbumID           *uint                  `json:"default_album_id,omitempty"`
	DefaultVisibility        *string                `json:"default_visibility,omitempty"`
	ConcurrentUploadLimit    *int                   `json:"concurrent_upload_limit,omitempty"`
	MaxFileSizeMB            *int                   `json:"max_file_size_mb,omitempty"`
	APIKeyEnabled            *bool                  `json:"api_key_enabled,omitempty"`
}

// UpdateConfig 更新转换配置（支持部分更新）
// @Summary      Update image processing configuration
// @Description  Update image processing settings (partial update supported)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      UpdateConfigRequest  true  "Image processing settings (partial)"
// @Success      200      {object}  common.Response  "Config updated"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/conversion [put]
func (h *ConversionHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	ctx := c.Request.Context()
	userID := c.GetUint(middleware.ContextUserIDKey)

	// 获取现有配置
	current, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to get current config: %v", err))
		return
	}

	// 合并更新（只更新提供的字段）
	if req.ThumbnailEnabled != nil {
		current.ThumbnailEnabled = *req.ThumbnailEnabled
	}
	if req.ThumbnailSizes != nil {
		current.ThumbnailSizes = req.ThumbnailSizes
	}
	if req.ThumbnailQuality != nil {
		current.ThumbnailQuality = *req.ThumbnailQuality
	}
	if req.ConversionEnabledFormats != nil {
		current.ConversionEnabledFormats = req.ConversionEnabledFormats
	}
	if req.WebPQuality != nil {
		current.WebPQuality = *req.WebPQuality
	}
	if req.WebPEffort != nil {
		current.WebPEffort = *req.WebPEffort
	}
	if req.AVIFQuality != nil {
		current.AVIFQuality = *req.AVIFQuality
	}
	if req.AVIFSpeed != nil {
		current.AVIFSpeed = *req.AVIFSpeed
	}
	if req.AVIFExperimental != nil {
		current.AVIFExperimental = *req.AVIFExperimental
	}
	if req.SkipSmallerThan != nil {
		current.SkipSmallerThan = *req.SkipSmallerThan
	}
	if req.MaxDimension != nil {
		current.MaxDimension = *req.MaxDimension
	}
	if req.DefaultAlbumID != nil {
		current.DefaultAlbumID = *req.DefaultAlbumID
	}
	if req.DefaultVisibility != nil {
		current.DefaultVisibility = *req.DefaultVisibility
	}
	if req.ConcurrentUploadLimit != nil {
		current.ConcurrentUploadLimit = *req.ConcurrentUploadLimit
	}
	if req.MaxFileSizeMB != nil {
		current.MaxFileSizeMB = *req.MaxFileSizeMB
	}
	if req.APIKeyEnabled != nil {
		current.APIKeyEnabled = *req.APIKeyEnabled
	}

	if err := h.configManager.SaveImageProcessingSettings(ctx, current, userID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to save config: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config updated"})
}
