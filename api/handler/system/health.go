package system

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var startTime = time.Now()

type HealthHandler struct {
	db              *gorm.DB
	storageProvider storage.Provider
}

func NewHealthHandler(db *gorm.DB, storageProvider storage.Provider) *HealthHandler {
	return &HealthHandler{
		db:              db,
		storageProvider: storageProvider,
	}
}

// Handle
// @Summary      Health check
// @Description  Check the health status of the application and its dependencies (database, cache, storage)
// @Tags         system
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any  "Service is healthy"
// @Success      503  {object}  map[string]any  "Service is unhealthy"
// @Router       /system/health [get]
func (h *HealthHandler) Handle(context *gin.Context) {
	sqlDB, _ := h.db.DB()
	checks := gin.H{
		"database": checkDatabaseHealth(sqlDB),
		"cache":    checkCacheHealth(),
		"storage":  h.checkStorageHealth(context.Request.Context()),
	}

	httpStatus := http.StatusOK
	status := "ok"
	for _, checkResult := range checks {
		if result, ok := checkResult.(string); ok && result != "ok" {
			httpStatus = http.StatusServiceUnavailable
			status = "error"
			break
		}
	}

	health := gin.H{
		"status":  status,
		"uptime":  time.Since(startTime).Round(time.Second).String(),
		"version": config.Version,
		"commit":  config.CommitHash,
		"checks":  checks,
	}
	context.JSON(httpStatus, health)
}

func checkDatabaseHealth(sqlDB *sql.DB) string {
	if sqlDB == nil {
		return "not initialized"
	}
	if err := sqlDB.Ping(); err != nil {
		return "unavailable: " + err.Error()
	}
	return "ok"
}

func checkCacheHealth() string {
	if cache.GetDefault() != nil {
		return "ok"
	}
	return "not initialized"
}

func (h *HealthHandler) checkStorageHealth(ctx context.Context) string {
	if h.storageProvider == nil {
		return "error: no default storage provider"
	}

	if err := h.storageProvider.Health(ctx); err != nil {
		return "error: " + err.Error()
	}

	return "ok"
}
