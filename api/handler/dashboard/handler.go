package dashboard

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/internal/dashboard"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *dashboard.Service
}

func NewHandler(svc *dashboard.Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

// GetStats
// @Description  Get dashboard statistics including image count, storage usage, and recent activity
// @Tags         dashboard
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Dashboard statistics"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/dashboard/stats [get]
func (h *Handler) GetStats(c *gin.Context) {
	// For regular users, scope to their own data
	// For admins, show global stats
	var userID *uint
	role := c.GetString(middleware.ContextRoleKey)
	if role != middleware.RoleAdmin {
		uid := c.GetUint(middleware.ContextUserIDKey)
		userID = &uid
	}

	stats, err := h.svc.GetStats(c.Request.Context(), userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}

	common.RespondSuccess(c, stats)
}

// RefreshStats
// @Summary      Refresh dashboard statistics
// @Description  Force refresh the dashboard statistics cache
// @Tags         dashboard
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Stats refreshed successfully"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/dashboard/stats/refresh [post]
func (h *Handler) RefreshStats(c *gin.Context) {
	var userID *uint
	role := c.GetString(middleware.ContextRoleKey)
	if role != middleware.RoleAdmin {
		uid := c.GetUint(middleware.ContextUserIDKey)
		userID = &uid
	}

	if err := h.svc.RefreshCache(c.Request.Context(), userID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to refresh stats")
		return
	}

	common.RespondSuccessMessage(c, "Stats refreshed successfully", nil)
}

func (h *Handler) SetupRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	dashGroup := router.Group("/dashboard")
	dashGroup.Use(authMiddleware)
	{
		dashGroup.GET("/stats", h.GetStats)
		dashGroup.POST("/stats/refresh", h.RefreshStats)
	}
}
