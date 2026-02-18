package admin

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config/db"
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
func (h *ConversionHandler) GetConfig(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.configManager.GetConversionSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		return
	}

	common.RespondSuccess(c, settings)
}

// UpdateConfig 更新转换配置
func (h *ConversionHandler) UpdateConfig(c *gin.Context) {
	var req config.ConversionSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetUint(middleware.ContextUserIDKey)

	if err := h.configManager.SaveConversionSettings(ctx, &req, userID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save config")
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config updated"})
}
