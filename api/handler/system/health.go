package system

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

var (
	startTime = time.Now()
	healthLog = utils.ForModule("Health")
)

type HealthHandler struct {
	sqlDB              *sql.DB
	getStorageProvider func() storage.Provider

	mu         sync.Mutex
	lastStatus map[string]string
}

func NewHealthHandler(sqlDB *sql.DB, getStorageProvider func() storage.Provider) *HealthHandler {
	if getStorageProvider == nil {
		getStorageProvider = func() storage.Provider { return nil }
	}
	return &HealthHandler{
		sqlDB:              sqlDB,
		getStorageProvider: getStorageProvider,
		lastStatus:         make(map[string]string),
	}
}

// logStatusChange 仅在某项检查状态变化时记录日志，避免公开健康接口被频繁轮询时刷屏。
func (h *HealthHandler) logStatusChange(check, status string, err error) {
	h.mu.Lock()
	prev, seen := h.lastStatus[check]
	h.lastStatus[check] = status
	h.mu.Unlock()

	if seen && prev == status {
		return
	}
	switch {
	case status != "ok":
		healthLog.Errorf("%s health check failed: %v", check, err)
	case seen:
		healthLog.Infof("%s health check recovered", check)
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
	checks := gin.H{
		"database": h.checkDatabaseHealth(),
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

func (h *HealthHandler) checkDatabaseHealth() string {
	if h.sqlDB == nil {
		return "not initialized"
	}
	if err := h.sqlDB.Ping(); err != nil {
		// 响应体不暴露底层错误细节，仅按状态变化记录服务端日志。
		h.logStatusChange("database", "unavailable", err)
		return "unavailable"
	}
	h.logStatusChange("database", "ok", nil)
	return "ok"
}

func checkCacheHealth() string {
	if cache.GetDefault() != nil {
		return "ok"
	}
	return "not initialized"
}

func (h *HealthHandler) checkStorageHealth(ctx context.Context) string {
	storageProvider := h.getStorageProvider()
	if storageProvider == nil {
		return "error: no default storage provider"
	}

	if err := storageProvider.Health(ctx); err != nil {
		// 响应体不暴露底层存储错误细节，仅按状态变化记录服务端日志。
		h.logStatusChange("storage", "error", err)
		return "error"
	}

	h.logStatusChange("storage", "ok", nil)
	return "ok"
}
