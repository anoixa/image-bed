package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/cache/redis"
	"github.com/anoixa/image-bed/database/models"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/storage/local"
	"github.com/anoixa/image-bed/storage/minio"
	"github.com/gin-gonic/gin"
)

// ConfigHandler 配置管理处理器
type ConfigHandler struct {
	manager *configSvc.Manager
}

// NewConfigHandler 创建配置处理器
func NewConfigHandler(manager *configSvc.Manager) *ConfigHandler {
	return &ConfigHandler{
		manager: manager,
	}
}

// ListConfigs 列出配置列表
// GET /api/v1/admin/configs
func (h *ConfigHandler) ListConfigs(c *gin.Context) {
	ctx := context.Background()

	// 解析查询参数
	category := c.Query("category")
	enabledOnly := c.Query("enabled_only") == "true"
	maskSensitive := c.Query("mask_sensitive") != "false" // 默认脱敏

	var cat models.ConfigCategory
	if category != "" {
		cat = models.ConfigCategory(category)
	}

	configs, err := h.manager.ListConfigs(ctx, cat, enabledOnly, maskSensitive)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to list configs: %v", err))
		return
	}

	common.RespondSuccess(c, configs)
}

// GetConfig 获取单个配置详情
// GET /api/v1/admin/configs/:id
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	maskSensitive := c.Query("mask_sensitive") != "false"

	config, err := h.manager.GetConfig(ctx, uint(id), maskSensitive)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, fmt.Sprintf("Config not found: %v", err))
		return
	}

	common.RespondSuccess(c, config)
}

// CreateConfig 创建新配置
// POST /api/v1/admin/configs
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	ctx := context.Background()

	var req models.SystemConfigStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	// 获取当前用户ID
	userID := c.GetUint("user_id")

	config, err := h.manager.CreateConfig(ctx, &req, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to create config: %v", err))
		return
	}

	common.RespondSuccess(c, config)
}

// UpdateConfig 更新配置
// PUT /api/v1/admin/configs/:id
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	var req models.SystemConfigStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	config, err := h.manager.UpdateConfig(ctx, uint(id), &req)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to update config: %v", err))
		return
	}

	common.RespondSuccess(c, config)
}

// DeleteConfig 删除配置
// DELETE /api/v1/admin/configs/:id
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	if err := h.manager.DeleteConfig(ctx, uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to delete config: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config deleted successfully"})
}

// SetDefaultConfig 设置默认配置
// POST /api/v1/admin/configs/:id/default
func (h *ConfigHandler) SetDefaultConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	if err := h.manager.SetDefault(ctx, uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to set default: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Default config set successfully"})
}

// EnableConfig 启用配置
// POST /api/v1/admin/configs/:id/enable
func (h *ConfigHandler) EnableConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	if err := h.manager.Enable(ctx, uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to enable config: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config enabled successfully"})
}

// DisableConfig 禁用配置
// POST /api/v1/admin/configs/:id/disable
func (h *ConfigHandler) DisableConfig(c *gin.Context) {
	ctx := context.Background()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	if err := h.manager.Disable(ctx, uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to disable config: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config disabled successfully"})
}

// TestConfig 测试配置连接
// POST /api/v1/admin/configs/test
func (h *ConfigHandler) TestConfig(c *gin.Context) {
	var req models.TestConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	result := h.testConfig(&req)
	common.RespondSuccess(c, result)
}

// testConfig 测试配置
func (h *ConfigHandler) testConfig(req *models.TestConfigRequest) *models.TestConfigResponse {
	switch req.Category {
	case models.ConfigCategoryStorage:
		return h.testStorageConfig(req.Config)
	case models.ConfigCategoryCache:
		return h.testCacheConfig(req.Config)
	default:
		return &models.TestConfigResponse{
			Success: false,
			Message: fmt.Sprintf("Unsupported category: %s", req.Category),
		}
	}
}

// testStorageConfig 测试存储配置
func (h *ConfigHandler) testStorageConfig(config map[string]interface{}) *models.TestConfigResponse {
	storageType, _ := config["type"].(string)
	if storageType == "" {
		return &models.TestConfigResponse{
			Success: false,
			Message: "Storage type is required",
		}
	}

	switch storageType {
	case "local":
		path := getString(config, "local_path")
		if path == "" {
			return &models.TestConfigResponse{
				Success: false,
				Message: "Local path is required",
			}
		}
		// 测试本地路径是否可写
		provider, err := local.NewStorage(path)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to create local storage: %v", err),
			}
		}
		ctx := context.Background()
		if err := provider.Health(ctx); err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Health check failed: %v", err),
			}
		}
		return &models.TestConfigResponse{
			Success: true,
			Message: "Local storage connection successful",
		}

	case "minio":
		minioCfg := minio.Config{
			Endpoint:        getString(config, "endpoint"),
			AccessKeyID:     getString(config, "access_key_id"),
			SecretAccessKey: getString(config, "secret_access_key"),
			UseSSL:          getBool(config, "use_ssl"),
			BucketName:      getString(config, "bucket_name"),
		}
		if minioCfg.Endpoint == "" || minioCfg.AccessKeyID == "" || minioCfg.SecretAccessKey == "" {
			return &models.TestConfigResponse{
				Success: false,
				Message: "Endpoint, access_key_id and secret_access_key are required",
			}
		}
		provider, err := minio.NewStorage(minioCfg)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to create minio storage: %v", err),
			}
		}
		ctx := context.Background()
		if err := provider.Health(ctx); err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Health check failed: %v", err),
			}
		}
		return &models.TestConfigResponse{
			Success: true,
			Message: "MinIO storage connection successful",
		}

	default:
		return &models.TestConfigResponse{
			Success: false,
			Message: fmt.Sprintf("Unsupported storage type: %s", storageType),
		}
	}
}

// testCacheConfig 测试缓存配置
func (h *ConfigHandler) testCacheConfig(config map[string]interface{}) *models.TestConfigResponse {
	providerType, _ := config["provider_type"].(string)
	if providerType == "" {
		providerType = "memory"
	}

	switch providerType {
	case "memory":
		// 内存缓存通常不会有连接问题
		return &models.TestConfigResponse{
			Success: true,
			Message: "Memory cache configuration is valid",
		}

	case "redis":
		redisCfg := &redis.Config{
			Address:      getString(config, "address"),
			Password:     getString(config, "password"),
			DB:           getInt(config, "db"),
			PoolSize:     getInt(config, "pool_size"),
			MinIdleConns: getInt(config, "min_idle_conns"),
		}
		if redisCfg.Address == "" {
			redisCfg.Address = "localhost:6379"
		}

		provider, err := redis.NewRedisFromConfig(redisCfg)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to connect to Redis: %v", err),
			}
		}
		defer provider.Close()

		ctx := context.Background()
		if err := provider.Health(ctx); err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Redis health check failed: %v", err),
			}
		}

		return &models.TestConfigResponse{
			Success: true,
			Message: "Redis connection successful",
		}

	default:
		return &models.TestConfigResponse{
			Success: false,
			Message: fmt.Sprintf("Unsupported cache provider: %s", providerType),
		}
	}
}

// ListStorageProviders 列出所有存储提供者
// GET /api/v1/admin/storage/providers
func (h *ConfigHandler) ListStorageProviders(c *gin.Context) {
	common.RespondSuccess(c, []map[string]interface{}{
		{
			"id":         0,
			"name":       "default",
			"type":       "local",
			"is_default": true,
		},
	})
}

// ReloadStorageConfig 热重载存储配置
// POST /api/v1/admin/storage/reload/:id
func (h *ConfigHandler) ReloadStorageConfig(c *gin.Context) {
	common.RespondSuccess(c, gin.H{"message": "Storage reload not supported in simplified mode"})
}

// 辅助函数
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
