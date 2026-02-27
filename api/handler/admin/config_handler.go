package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/api/common"
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
// @Summary      List system configurations
// @Description  Get list of system configurations including storage and image processing settings
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        category        query     string  false  "Filter by category: storage, image_processing"
// @Param        enabled_only    query     bool    false  "Only show enabled configs"
// @Param        mask_sensitive  query     bool    false  "Mask sensitive information (default: true)"
// @Success      200  {object}  common.Response  "Configuration list"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs [get]
func (h *ConfigHandler) ListConfigs(c *gin.Context) {
	ctx := c.Request.Context()

	category := c.Query("category")
	enabledOnly := c.Query("enabled_only") == "true"
	maskSensitive := c.Query("mask_sensitive") != "false"

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
// @Summary      Get configuration details
// @Description  Get detailed information about a specific configuration
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id              path      int   true   "Config ID"
// @Param        mask_sensitive  query     bool  false  "Mask sensitive information (default: true)"
// @Success      200  {object}  common.Response  "Configuration details"
// @Failure      400  {object}  common.Response  "Invalid config ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "Config not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id} [get]
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	ctx := c.Request.Context()

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
// @Summary      Create configuration
// @Description  Create a new system configuration (storage or image processing)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      models.SystemConfigStoreRequest  true  "Configuration data"
// @Success      200      {object}  common.Response  "Configuration created successfully"
// @Failure      400      {object}  common.Response  "Invalid request or configuration test failed"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs [post]
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	ctx := c.Request.Context()

	var req models.SystemConfigStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	userID := c.GetUint("user_id")

	// test connection
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

	if req.Category == models.ConfigCategoryStorage {
		if err := h.hotReloadStorageConfig(config.ID, req.Config, config.IsDefault); err != nil {
			if rollbackErr := h.manager.DeleteConfig(c.Request.Context(), config.ID); rollbackErr != nil {
				log.Printf("Failed to rollback storage config creation: %v", rollbackErr)
			}
			common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to load storage configuration: %v", err))
			return
		}
	}

	common.RespondSuccess(c, config)
}

// UpdateConfig 更新配置
// @Summary      Update configuration
// @Description  Update an existing system configuration
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id       path      int                              true  "Config ID"
// @Param        request  body      models.SystemConfigStoreRequest  true  "Configuration data"
// @Success      200      {object}  common.Response  "Configuration updated successfully"
// @Failure      400      {object}  common.Response  "Invalid request or configuration test failed"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      404      {object}  common.Response  "Config not found"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id} [put]
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	ctx := c.Request.Context()

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
// @Summary      Delete configuration
// @Description  Delete a system configuration by ID
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Config ID"
// @Success      200  {object}  common.Response  "Configuration deleted successfully"
// @Failure      400  {object}  common.Response  "Invalid config ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "Config not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id} [delete]
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	config, getErr := h.manager.GetConfig(c.Request.Context(), uint(id), false)
	if getErr == nil && config.Category == models.ConfigCategoryStorage {
		if err := storage.RemoveProvider(uint(id)); err != nil {
			if !strings.Contains(err.Error(), "not found") {
				log.Printf("Warning: failed to remove storage provider: %v", err)
			}
		}
	}

	if err := h.manager.DeleteConfig(c.Request.Context(), uint(id)); err != nil {
		common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to delete config: %v", err))
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config deleted successfully"})
}

// SetDefaultConfig 设置默认配置
// @Summary      Set default configuration
// @Description  Set a configuration as the default for its category
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Config ID"
// @Success      200  {object}  common.Response  "Default config set successfully"
// @Failure      400  {object}  common.Response  "Invalid config ID or storage provider not loaded"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "Config not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id}/default [post]
func (h *ConfigHandler) SetDefaultConfig(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	config, err := h.manager.GetConfig(ctx, uint(id), false)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, fmt.Sprintf("Config not found: %v", err))
		return
	}

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

	if config.Category == models.ConfigCategoryStorage {
		if err := storage.SetDefaultID(uint(id)); err != nil {
			common.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to set default storage: %v", err))
			return
		}
	}

	common.RespondSuccess(c, gin.H{"message": "Default config set successfully"})
}

// EnableConfig 启用配置
// @Summary      Enable configuration
// @Description  Enable a previously disabled configuration
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Config ID"
// @Success      200  {object}  common.Response  "Config enabled successfully"
// @Failure      400  {object}  common.Response  "Invalid config ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id}/enable [post]
func (h *ConfigHandler) EnableConfig(c *gin.Context) {
	ctx := c.Request.Context()

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
// @Summary      Disable configuration
// @Description  Disable a configuration without deleting it
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Config ID"
// @Success      200  {object}  common.Response  "Config disabled successfully"
// @Failure      400  {object}  common.Response  "Invalid config ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /admin/configs/{id}/disable [post]
func (h *ConfigHandler) DisableConfig(c *gin.Context) {
	ctx := c.Request.Context()

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
// @Summary      Test configuration
// @Description  Test a configuration without saving it (storage connection test)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      models.TestConfigRequest  true  "Configuration to test"
// @Success      200      {object}  models.TestConfigResponse  "Test result"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Security     ApiKeyAuth
// @Router       /admin/configs/test [post]
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
	case models.ConfigCategoryImageProcessing:
		return &models.TestConfigResponse{
			Success: true,
			Message: "Image processing configuration cannot be tested directly",
		}
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

	case "webdav":
		webdavCfg := storage.WebDAVConfig{
			URL:      getString(config, "webdav_url"),
			Username: getString(config, "webdav_username"),
			Password: getString(config, "webdav_password"),
			RootPath: getString(config, "webdav_root_path"),
			Timeout:  10 * time.Second,
		}
		if webdavCfg.URL == "" {
			return &models.TestConfigResponse{
				Success: false,
				Message: "WebDAV URL is required",
			}
		}
		provider, err := storage.NewWebDAVStorage(webdavCfg)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to create WebDAV storage: %v", err),
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
			Message: "WebDAV storage connection successful",
		}

	default:
		return &models.TestConfigResponse{
			Success: false,
			Message: fmt.Sprintf("Unsupported storage type: %s", storageType),
		}
	}
}

// ListStorageProviders 列出所有存储提供者
// @Summary      List storage providers
// @Description  Get list of all loaded storage providers
// @Tags         admin
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Storage provider list"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Security     ApiKeyAuth
// @Router       /admin/storage/providers [get]
func (h *ConfigHandler) ListStorageProviders(c *gin.Context) {
	providers := storage.ListProviders()
	result := make([]map[string]interface{}, 0, len(providers))
	for _, p := range providers {
		result = append(result, map[string]interface{}{
			"id":         p.ID,
			"name":       p.Name,
			"type":       p.Type,
			"is_default": p.IsDefault,
		})
	}
	common.RespondSuccess(c, result)
}

// ReloadStorageConfig 热重载存储配置
// @Summary      Reload storage configuration
// @Description  Hot reload a storage configuration (not supported in simplified mode)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Storage config ID"
// @Success      200  {object}  common.Response  "Reload status"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Security     ApiKeyAuth
// @Router       /admin/storage/reload/{id} [post]
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
	case "webdav":
		cfg.WebDAVURL = getString(config, "webdav_url")
		cfg.WebDAVUsername = getString(config, "webdav_username")
		cfg.WebDAVPassword = getString(config, "webdav_password")
		cfg.WebDAVRootPath = getString(config, "webdav_root_path")
		if cfg.WebDAVURL == "" {
			return fmt.Errorf("webdav_url is required for webdav storage")
		}
	default:
		return fmt.Errorf("unsupported storage type: %s", storageType)
	}

	return storage.AddOrUpdateProvider(cfg)
}

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
