package admin

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	config "github.com/anoixa/image-bed/config/db"
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
// @Router       /admin/conversion/config [get]
func (h *ConversionHandler) GetConfig(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		return
	}

	common.RespondSuccess(c, settings)
}

// UpdateConfig 更新转换配置
// @Summary      Update image processing configuration
// @Description  Update image processing settings including thumbnail and WebP conversion options
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      config.ImageProcessingSettings  true  "Image processing settings"
// @Success      200      {object}  common.Response  "Config updated"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/conversion/config [put]
func (h *ConversionHandler) UpdateConfig(c *gin.Context) {
	var req config.ImageProcessingSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetUint(middleware.ContextUserIDKey)

	if err := h.configManager.SaveImageProcessingSettings(ctx, &req, userID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save config")
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config updated"})
}
