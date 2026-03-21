package admin

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/gin-gonic/gin"
)

// SettingsHandler 用户设置处理器
type SettingsHandler struct {
	configManager *config.Manager
}

// NewSettingsHandler 创建设置处理器
func NewSettingsHandler(cm *config.Manager) *SettingsHandler {
	return &SettingsHandler{configManager: cm}
}

// GetSettings 获取用户设置
// @Summary      Get user settings
// @Description  Get current user application settings (WebP, upload limits, API key, etc.)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response{data=config.UserSettings}  "User settings"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/settings [get]
func (h *SettingsHandler) GetSettings(c *gin.Context) {
	ctx := c.Request.Context()

	settings, err := h.configManager.GetUserSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	common.RespondSuccess(c, settings)
}

// UpdateSettings 更新用户设置
// @Summary      Update user settings
// @Description  Update user application settings including WebP toggle, upload limits, and API key settings
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      config.UserSettings  true  "User settings"
// @Success      200      {object}  common.Response  "Settings updated"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/settings [put]
func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var req config.UserSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	userID := c.GetUint(middleware.ContextUserIDKey)

	if err := h.configManager.SaveUserSettings(ctx, &req, userID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save settings: "+err.Error())
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Settings updated successfully"})
}
