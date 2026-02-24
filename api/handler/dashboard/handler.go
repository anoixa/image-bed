package dashboard

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/internal/dashboard"
	"github.com/gin-gonic/gin"
)

// Handler Dashboard 处理器
type Handler struct {
	svc *dashboard.Service
}

// NewHandler 创建新的 Dashboard 处理器
func NewHandler(svc *dashboard.Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

// GetStats 获取 Dashboard 统计数据
// GET /api/v1/dashboard/stats
func (h *Handler) GetStats(c *gin.Context) {
	stats, err := h.svc.GetStats(c.Request.Context())
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}

	common.RespondSuccess(c, stats)
}

// RefreshStats 刷新 Dashboard 统计缓存
// POST /api/v1/dashboard/stats/refresh
func (h *Handler) RefreshStats(c *gin.Context) {
	if err := h.svc.RefreshCache(c.Request.Context()); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to refresh stats")
		return
	}

	common.RespondSuccessMessage(c, "Stats refreshed successfully", nil)
}

// SetupRoutes 设置 Dashboard 路由
func (h *Handler) SetupRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	dashboard := router.Group("/dashboard")
	dashboard.Use(authMiddleware)
	{
		dashboard.GET("/stats", h.GetStats)
		dashboard.POST("/stats/refresh", h.RefreshStats)
	}
}
