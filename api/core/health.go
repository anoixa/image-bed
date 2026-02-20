package core

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

// HealthHandler 健康检查处理器
type HealthHandler struct {
	db *gorm.DB
}

// NewHealthHandler 创建健康检查处理器
func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Handle 处理健康检查请求
func (h *HealthHandler) Handle(context *gin.Context) {
	sqlDB, _ := h.db.DB()
	health := gin.H{
		"status":  "ok",
		"uptime":  time.Since(startTime).Round(time.Second).String(),
		"version": config.Version,
		"commit":  config.CommitHash,
		"checks": gin.H{
			"database": checkDatabaseHealth(sqlDB),
			"cache":    checkCacheHealth(),
			"storage":  checkStorageHealth(),
		},
	}
	httpStatus := http.StatusOK
	for _, checkResult := range health["checks"].(gin.H) {
		if result, ok := checkResult.(string); ok && result != "ok" {
			httpStatus = http.StatusServiceUnavailable
			break
		}
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

func checkStorageHealth() string {
	provider := storage.GetDefault()
	if provider == nil {
		return "error: no default storage provider"
	}

	ctx := context.Background()
	if err := provider.Health(ctx); err != nil {
		return "error: " + err.Error()
	}

	return "ok"
}
