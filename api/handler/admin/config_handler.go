package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/database/models"
	imagesRepo "github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type configManager interface {
	ListConfigs(ctx context.Context, category models.ConfigCategory, enabledOnly, maskSensitive bool) ([]*models.ConfigResponse, error)
	GetConfig(ctx context.Context, id uint, maskSensitive bool) (*models.ConfigResponse, error)
	CreateConfig(ctx context.Context, req *models.SystemConfigStoreRequest, userID uint) (*models.ConfigResponse, error)
	UpdateConfig(ctx context.Context, id uint, req *models.SystemConfigStoreRequest) (*models.ConfigResponse, error)
	DeleteConfig(ctx context.Context, id uint) error
	SetDefault(ctx context.Context, id uint) error
	Enable(ctx context.Context, id uint) error
	Disable(ctx context.Context, id uint) error
	GetGlobalTransferMode(ctx context.Context) storage.TransferMode
	SetGlobalTransferMode(ctx context.Context, mode storage.TransferMode) error
	ClearCache()
}

// ConfigHandler 配置管理处理器
type ConfigHandler struct {
	manager             configManager
	imagesRepo          *imagesRepo.Repository
	reloadStorageConfig func(id uint, config map[string]any, isDefault bool) error
}

var hiddenExternalConfigCategories = map[models.ConfigCategory]struct{}{
	models.ConfigCategoryJWT:      {},
	models.ConfigCategorySecurity: {},
}

// NewConfigHandler 创建配置处理器
func NewConfigHandler(manager configManager, imagesRepo *imagesRepo.Repository) *ConfigHandler {
	handler := &ConfigHandler{
		manager:    manager,
		imagesRepo: imagesRepo,
	}
	handler.reloadStorageConfig = handler.hotReloadStorageConfig
	return handler
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
// @Router       /api/v1/admin/configs [get]
func (h *ConfigHandler) ListConfigs(c *gin.Context) {
	ctx := c.Request.Context()

	category := c.Query("category")
	enabledOnly := c.Query("enabled_only") == "true"
	maskSensitive := c.Query("mask_sensitive") != "false"

	var cat models.ConfigCategory
	if category != "" {
		cat = models.ConfigCategory(category)
		if isHiddenExternalConfigCategory(cat) {
			common.RespondError(c, http.StatusBadRequest, "Unsupported config category")
			return
		}
	}

	configs, err := h.manager.ListConfigs(ctx, cat, enabledOnly, maskSensitive)
	if err != nil {
		adminConfigLog.Errorf("Failed to list configs: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to list configs")
		return
	}

	configs = filterVisibleConfigs(configs)

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
// @Router       /api/v1/admin/configs/{id} [get]
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
		adminConfigLog.Errorf("Failed to get config %d: %v", id, err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Config not found")
		} else {
			common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		}
		return
	}
	if isHiddenExternalConfigCategory(config.Category) {
		common.RespondError(c, http.StatusNotFound, "Config not found")
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
// @Router       /api/v1/admin/configs [post]
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	ctx := c.Request.Context()

	var req models.SystemConfigStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}
	if isHiddenExternalConfigCategory(req.Category) {
		common.RespondError(c, http.StatusBadRequest, "Unsupported config category")
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
		adminConfigLog.Errorf("Failed to create config: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to create config")
		return
	}

	if req.Category == models.ConfigCategoryStorage {
		if err := h.reloadStorageConfig(config.ID, req.Config, config.IsDefault); err != nil {
			if rollbackErr := h.manager.DeleteConfig(c.Request.Context(), config.ID); rollbackErr != nil {
				adminConfigLog.Errorf("Failed to rollback storage config creation: %v", rollbackErr)
			}
			adminConfigLog.Errorf("Failed to load storage configuration: %v", err)
			common.RespondError(c, http.StatusInternalServerError, "Failed to load storage configuration")
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
// @Router       /api/v1/admin/configs/{id} [put]
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	var req models.SystemConfigStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}
	if isHiddenExternalConfigCategory(req.Category) {
		common.RespondError(c, http.StatusBadRequest, "Unsupported config category")
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
		adminConfigLog.Errorf("Failed to update config: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to update config")
		return
	}

	// 如果是存储配置，热重载到存储层
	if req.Category == models.ConfigCategoryStorage {
		if err := h.reloadStorageConfig(config.ID, req.Config, config.IsDefault); err != nil {
			adminConfigLog.Errorf("Failed to reload storage configuration: %v", err)
			common.RespondError(c, http.StatusInternalServerError, "Failed to reload storage configuration")
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
// @Router       /api/v1/admin/configs/{id} [delete]
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	config, getErr := h.manager.GetConfig(c.Request.Context(), uint(id), false)
	if getErr == nil && isHiddenExternalConfigCategory(config.Category) {
		common.RespondError(c, http.StatusNotFound, "Config not found")
		return
	}
	if getErr == nil && config.Category == models.ConfigCategoryStorage {
		// 检查是否有图片使用该存储配置
		if h.imagesRepo != nil {
			count, err := h.imagesRepo.CountImagesByStorageConfig(uint(id))
			if err != nil {
				adminConfigLog.Errorf("Failed to check associated images: %v", err)
				common.RespondError(c, http.StatusInternalServerError, "Failed to check associated images")
				return
			}
			if count > 0 {
				common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Cannot delete storage config: %d image(s) are still using this storage. Please migrate or delete these images first.", count))
				return
			}
		}

		if err := storage.RemoveProvider(uint(id)); err != nil {
			if !strings.Contains(err.Error(), "not found") {
				adminConfigLog.Warnf("Failed to remove storage provider: %v", err)
			}
		}
	}

	if err := h.manager.DeleteConfig(c.Request.Context(), uint(id)); err != nil {
		adminConfigLog.Errorf("Failed to delete config: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete config")
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
// @Router       /api/v1/admin/configs/{id}/default [post]
func (h *ConfigHandler) SetDefaultConfig(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	config, err := h.manager.GetConfig(ctx, uint(id), false)
	if err != nil {
		adminConfigLog.Errorf("Failed to get config %d before set default: %v", id, err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Config not found")
		} else {
			common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		}
		return
	}
	if isHiddenExternalConfigCategory(config.Category) {
		common.RespondError(c, http.StatusNotFound, "Config not found")
		return
	}

	if config.Category == models.ConfigCategoryStorage {
		_, err := storage.GetByID(uint(id))
		if err != nil {
			// Provider 未加载，尝试热重载
			if loadErr := h.reloadStorageConfig(config.ID, config.Config, false); loadErr != nil {
				adminConfigLog.Errorf("Storage provider not loaded and failed to reload: %v", loadErr)
				common.RespondError(c, http.StatusBadRequest, "Storage provider not loaded and failed to reload")
				return
			}
		}
	}

	if err := h.manager.SetDefault(ctx, uint(id)); err != nil {
		adminConfigLog.Errorf("Failed to set default: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to set default")
		return
	}

	if config.Category == models.ConfigCategoryStorage {
		if err := storage.SetDefaultID(uint(id)); err != nil {
			adminConfigLog.Errorf("Failed to set default storage: %v", err)
			common.RespondError(c, http.StatusInternalServerError, "Failed to set default storage")
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
// @Router       /api/v1/admin/configs/{id}/enable [post]
func (h *ConfigHandler) EnableConfig(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	// 先获取配置信息，用于后续热重载
	config, getErr := h.manager.GetConfig(ctx, uint(id), false)
	if getErr != nil {
		adminConfigLog.Errorf("Failed to get config: %v", getErr)
		common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		return
	}
	if isHiddenExternalConfigCategory(config.Category) {
		common.RespondError(c, http.StatusNotFound, "Config not found")
		return
	}

	if err := h.manager.Enable(ctx, uint(id)); err != nil {
		adminConfigLog.Errorf("Failed to enable config: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to enable config")
		return
	}

	// 如果是存储配置，启用后热重载到内存
	if config.Category == models.ConfigCategoryStorage {
		if err := h.reloadStorageConfig(config.ID, config.Config, config.IsDefault); err != nil {
			// 热重载失败但不回滚启用操作，只是记录日志
			adminConfigLog.Debugf("Failed to hot reload storage config %d after enable: %v", config.ID, err)
		}
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
// @Router       /api/v1/admin/configs/{id}/disable [post]
func (h *ConfigHandler) DisableConfig(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	config, getErr := h.manager.GetConfig(ctx, uint(id), false)
	if getErr != nil {
		adminConfigLog.Errorf("Failed to get config: %v", getErr)
		if errors.Is(getErr, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Config not found")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get config")
		return
	}
	if isHiddenExternalConfigCategory(config.Category) {
		common.RespondError(c, http.StatusNotFound, "Config not found")
		return
	}

	if err := h.manager.Disable(ctx, uint(id)); err != nil {
		adminConfigLog.Errorf("Failed to disable config: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to disable config")
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Config disabled successfully"})
}

// TestConfig 测试配置连接
// @Summary      Test configuration
// @Description  Test a configuration without saving it (storage connection test).
// @Description  If no body is provided, tests the existing configuration by ID.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id       path      int                       true   "Config ID"
// @Param        request  body      models.TestConfigRequest  false  "Configuration to test (optional)"
// @Success      200      {object}  models.TestConfigResponse  "Test result"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/configs/{id}/test [post]
func (h *ConfigHandler) TestConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid config ID")
		return
	}

	var req models.TestConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 如果 body 为空或解析失败，尝试从数据库获取配置
		config, getErr := h.manager.GetConfig(c.Request.Context(), uint(id), false)
		if getErr != nil {
			common.RespondError(c, http.StatusBadRequest, "Invalid request body")
			return
		}
		req = models.TestConfigRequest{
			Category: config.Category,
			Config:   config.Config,
		}
	}
	if isHiddenExternalConfigCategory(req.Category) {
		common.RespondError(c, http.StatusBadRequest, "Unsupported config category")
		return
	}

	result := h.testConfig(&req)
	common.RespondSuccess(c, result)
}

func isHiddenExternalConfigCategory(category models.ConfigCategory) bool {
	_, hidden := hiddenExternalConfigCategories[category]
	return hidden
}

func filterVisibleConfigs(configs []*models.ConfigResponse) []*models.ConfigResponse {
	if len(configs) == 0 {
		return configs
	}

	filtered := make([]*models.ConfigResponse, 0, len(configs))
	for _, config := range configs {
		if config == nil || isHiddenExternalConfigCategory(config.Category) {
			continue
		}
		filtered = append(filtered, config)
	}
	return filtered
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
func (h *ConfigHandler) testStorageConfig(config map[string]any) *models.TestConfigResponse {
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

	case "s3":
		s3Cfg := storage.S3Config{
			Type:            storageType,
			Endpoint:        getString(config, "endpoint"),
			Region:          getString(config, "region"),
			BucketName:      getString(config, "bucket_name"),
			AccessKeyID:     getString(config, "access_key_id"),
			SecretAccessKey: getString(config, "secret_access_key"),
			ForcePathStyle:  getBool(config, "force_path_style"),
			PublicDomain:    getString(config, "public_domain"),
			IsPrivate:       getBool(config, "is_private"),
		}
		if s3Cfg.Endpoint == "" || s3Cfg.AccessKeyID == "" || s3Cfg.SecretAccessKey == "" || s3Cfg.BucketName == "" {
			return &models.TestConfigResponse{
				Success: false,
				Message: "Endpoint, access_key_id, secret_access_key and bucket_name are required",
			}
		}
		if err := validateRemoteStorageTestTarget(s3Cfg.Endpoint); err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: err.Error(),
			}
		}
		provider, err := storage.NewS3Storage(s3Cfg)
		if err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to create S3 storage: %v", err),
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
			Message: "S3 storage connection successful",
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
		if err := validateRemoteStorageTestTarget(webdavCfg.URL); err != nil {
			return &models.TestConfigResponse{
				Success: false,
				Message: err.Error(),
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

func validateRemoteStorageTestTarget(rawTarget string) error {
	targetURL, err := parseRemoteStorageTestTarget(rawTarget)
	if err != nil {
		return fmt.Errorf("invalid remote storage address: %w", err)
	}

	host := strings.TrimSuffix(strings.ToLower(targetURL.Hostname()), ".")
	if host == "" {
		return fmt.Errorf("invalid remote storage address: missing host")
	}

	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("refusing to test local or private address: %s", host)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %s: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedRemoteStorageTestIP(ip) {
			return fmt.Errorf("refusing to test private/reserved address: %s resolves to %s", host, ip)
		}
	}

	return nil
}

func parseRemoteStorageTestTarget(rawTarget string) (*url.URL, error) {
	normalized := strings.TrimSpace(rawTarget)
	if normalized == "" {
		return nil, fmt.Errorf("empty target")
	}
	if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	return parsed, nil
}

func isBlockedRemoteStorageTestIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	return isIPInCIDRs(ip, []string{
		"100.64.0.0/10",
		"198.18.0.0/15",
	})
}

func isIPInCIDRs(ip net.IP, cidrs []string) bool {
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
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
// @Router       /api/v1/admin/storage/providers [get]
func (h *ConfigHandler) ListStorageProviders(c *gin.Context) {
	providers := storage.ListProviders()
	result := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		result = append(result, map[string]any{
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
// @Router       /api/v1/admin/storage/reload/{id} [post]
func (h *ConfigHandler) ReloadStorageConfig(c *gin.Context) {
	common.RespondSuccess(c, gin.H{"message": "Storage reload not supported in simplified mode"})
}

// hotReloadStorageConfig 热重载存储配置
func (h *ConfigHandler) hotReloadStorageConfig(id uint, config map[string]any, isDefault bool) error {
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
	case "s3":
		cfg.Endpoint = getString(config, "endpoint")
		cfg.Region = getString(config, "region")
		cfg.BucketName = getString(config, "bucket_name")
		cfg.AccessKeyID = getString(config, "access_key_id")
		cfg.SecretAccessKey = getString(config, "secret_access_key")
		cfg.ForcePathStyle = getBool(config, "force_path_style")
		cfg.PublicDomain = getString(config, "public_domain")
		cfg.IsPrivate = getBool(config, "is_private")
		if cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.BucketName == "" {
			return fmt.Errorf("endpoint, access_key_id, secret_access_key and bucket_name are required for S3 storage")
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

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// === 全局转发模式配置 ===

// GetGlobalTransferMode 获取全局转发模式
// @Summary      Get global transfer mode
// @Description  Get the global image transfer mode (auto, always_proxy, always_direct)
// @Tags         admin
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response{data=map[string]string}  "Transfer mode"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/transfer-mode [get]
func (h *ConfigHandler) GetGlobalTransferMode(c *gin.Context) {
	ctx := c.Request.Context()
	mode := h.manager.GetGlobalTransferMode(ctx)

	common.RespondSuccess(c, map[string]string{
		"mode": string(mode),
	})
}

// SetGlobalTransferMode 设置全局转发模式
// @Summary      Set global transfer mode
// @Description  Set the global image transfer mode
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      SetTransferModeRequest  true  "Transfer mode request"
// @Success      200  {object}  common.Response  "Success"
// @Failure      400  {object}  common.Response  "Invalid mode"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/transfer-mode [post]
func (h *ConfigHandler) SetGlobalTransferMode(c *gin.Context) {
	var req SetTransferModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 验证模式值
	mode := storage.TransferMode(req.Mode)
	switch mode {
	case storage.TransferModeAuto, storage.TransferModeAlwaysProxy, storage.TransferModeAlwaysDirect:
		// valid
	default:
		common.RespondError(c, http.StatusBadRequest, "Invalid mode. Must be: auto, always_proxy, or always_direct")
		return
	}

	ctx := c.Request.Context()
	if err := h.manager.SetGlobalTransferMode(ctx, mode); err != nil {
		adminConfigLog.Errorf("Failed to set transfer mode: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to set transfer mode")
		return
	}

	// 清除配置缓存，使新配置立即生效
	h.manager.ClearCache()

	common.RespondSuccess(c, map[string]string{
		"mode": req.Mode,
	})
}

// SetTransferModeRequest 设置转发模式请求
type SetTransferModeRequest struct {
	Mode string `json:"mode" binding:"required"` // auto, always_proxy, always_direct
}
