package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/cache"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
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

	userID := c.GetUint("user_id")

	// 如果是存储配置，先测试连接再创建
	if req.Category == models.ConfigCategoryStorage {
		testResult := h.testConfig(&models.TestConfigRequest{
			Category: req.Category,
			Config:   req.Config,
		})
		if !testResult.Success {
			common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Storage configuration test failed: %s", testResult.Message))
			return
		}
	}

	config, err := h.manager.CreateConfig(ctx, &req, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to create config: %v", err))
		return
	}

	// 如果是存储配置，热加载到存储层
	if req.Category == models.ConfigCategoryStorage {
		if err := h.hotReloadStorageConfig(config.ID, req.Config, config.IsDefault); err != nil {
			// 热加载失败，回滚数据库操作
			if rollbackErr := h.manager.DeleteConfig(ctx, config.ID); rollbackErr != nil {
				log.Printf("Failed to rollback storage config creation: %v", rollbackErr)
			}
			common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to load storage configuration: %v", err))
			return
		}
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

	// 如果是存储配置，先测试连接再更新
	if req.Category == models.ConfigCategoryStorage {
		testResult := h.testConfig(&models.TestConfigRequest{
			Category: req.Category,
			Config:   req.Config,
		})
		if !testResult.Success {
			common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Storage configuration test failed: %s", testResult.Message))
			return
		}
	}

	config, err := h.manager.UpdateConfig(ctx, uint(id), &req)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to update config: %v", err))
		return
	}

	// 如果是存储配置，热重载到存储层
	if req.Category == models.ConfigCategoryStorage {
		if err := h.hotReloadStorageConfig(config.ID, req.Config, config.IsDefault); err != nil {
			common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to reload storage configuration: %v", err))
			return
		}
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

	// 如果是存储配置，先从存储层移除
	config, getErr := h.manager.GetConfig(ctx, uint(id), false)
	if getErr == nil && config.Category == models.ConfigCategoryStorage {
		if err := storage.RemoveProvider(uint(id)); err != nil {
			// 如果不是"not found"错误，说明有其他问题
			if !strings.Contains(err.Error(), "not found") {
				log.Printf("Warning: failed to remove storage provider: %v", err)
			}
		}
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

	// 获取配置信息，检查是否为存储配置
	config, err := h.manager.GetConfig(ctx, uint(id), false)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, fmt.Sprintf("Config not found: %v", err))
		return
	}

	// 如果是存储配置，先检查存储提供者是否存在
	if config.Category == models.ConfigCategoryStorage {
		_, err := storage.GetByID(uint(id))
		if err != nil {
			common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Storage provider not loaded: %v", err))
			return
		}
	}

	if err := h.manager.SetDefault(ctx, uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to set default: %v", err))
		return
	}

	// 如果是存储配置，同时切换存储层的默认存储
	if config.Category == models.ConfigCategoryStorage {
		if err := storage.SetDefaultID(uint(id)); err != nil {
			common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to set default storage: %v", err))
			return
		}
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
		provider, err := storage.NewLocalStorage(path)
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
		minioCfg := storage.MinioConfig{
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
		provider, err := storage.NewMinioStorage(minioCfg)
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
		redisCfg := &cache.RedisConfig{
			Address:      getString(config, "address"),
			Password:     getString(config, "password"),
			DB:           getInt(config, "db"),
			PoolSize:     getInt(config, "pool_size"),
			MinIdleConns: getInt(config, "min_idle_conns"),
		}
		if redisCfg.Address == "" {
			redisCfg.Address = "localhost:6379"
		}

		provider, err := cache.NewRedisCache(*redisCfg)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to connect to Redis: %v", err),
			}
		}
		defer func() { _ = provider.Close() }()

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

// hotReloadStorageConfig 热重载存储配置
func (h *ConfigHandler) hotReloadStorageConfig(id uint, config map[string]interface{}, isDefault bool) error {
	storageType := getString(config, "type")
	if storageType == "" {
		return fmt.Errorf("storage type is required")
	}

	cfg := storage.StorageConfig{
		ID:        id,
		Name:      getString(config, "name"),
		Type:      storageType,
		IsDefault: isDefault,
	}

	switch storageType {
	case "local":
		cfg.LocalPath = getString(config, "local_path")
		if cfg.LocalPath == "" {
			return fmt.Errorf("local_path is required for local storage")
		}
	case "minio":
		cfg.Endpoint = getString(config, "endpoint")
		cfg.AccessKeyID = getString(config, "access_key_id")
		cfg.SecretAccessKey = getString(config, "secret_access_key")
		cfg.UseSSL = getBool(config, "use_ssl")
		cfg.BucketName = getString(config, "bucket_name")
		if cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
			return fmt.Errorf("endpoint, access_key_id and secret_access_key are required for minio storage")
		}
	default:
		return fmt.Errorf("unsupported storage type: %s", storageType)
	}

	return storage.AddOrUpdateProvider(cfg)
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
