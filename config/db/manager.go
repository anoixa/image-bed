package config

import (
	"context"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/configs"
	cryptoservice "github.com/anoixa/image-bed/internal/crypto"
	"github.com/anoixa/image-bed/storage"
	cryptoutils "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// Manager 配置管理器
type Manager struct {
	db       *gorm.DB
	repo     configs.Repository
	crypto   *CryptoLayer
	cache    *CacheLayer
	eventBus *EventBus
	dataPath string
}

// JWTConfig JWT 配置结构
type JWTConfig struct {
	Secret          string
	AccessTokenTTL  string
	RefreshTokenTTL string
}

// NewManager 创建配置管理器
func NewManager(db *gorm.DB, dataPath string) *Manager {
	repo := configs.NewRepository(db)
	cryptoSvc := cryptoservice.NewService(dataPath)

	return &Manager{
		db:       db,
		repo:     repo,
		crypto:   NewCryptoLayer(repo, cryptoSvc),
		cache:    NewCacheLayer(),
		eventBus: NewEventBus(),
		dataPath: dataPath,
	}
}

// NewManagerWithCache 创建带缓存的配置管理器
func NewManagerWithCache(db *gorm.DB, dataPath string, cacheProvider cache.Provider, cacheTTL int) *Manager {
	repo := configs.NewRepository(db)
	cryptoSvc := cryptoservice.NewService(dataPath)

	return &Manager{
		db:       db,
		repo:     repo,
		crypto:   NewCryptoLayer(repo, cryptoSvc),
		cache:    NewCacheLayer(),
		eventBus: NewEventBus(),
		dataPath: dataPath,
	}
}

// Initialize 初始化配置
func (m *Manager) Initialize() error {
	if err := m.crypto.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize crypto: %w", err)
	}

	ctx := context.Background()

	if err := m.ensureDefaultImageProcessingConfig(ctx); err != nil {
		return fmt.Errorf("failed to ensure image processing config: %w", err)
	}

	if err := m.ensureDefaultLocalStorageConfig(ctx); err != nil {
		return fmt.Errorf("failed to ensure local storage config: %w", err)
	}

	log.Println("[ConfigManager] Initialized successfully")
	return nil
}

// GetRepo 获取仓库
func (m *Manager) GetRepo() configs.Repository {
	return m.repo
}

// GetCrypto 获取加密服务
func (m *Manager) GetCrypto() *cryptoservice.Service {
	return m.crypto.crypto
}

// Subscribe 订阅配置变更事件
func (m *Manager) Subscribe(eventType EventType, handler EventHandler) {
	m.eventBus.Subscribe(eventType, handler)
}

// CreateConfig 创建配置
func (m *Manager) CreateConfig(ctx context.Context, req *models.SystemConfigStoreRequest, userID uint) (*models.ConfigResponse, error) {
	baseKey := fmt.Sprintf("%s:%s", req.Category, req.Name)
	key, err := m.repo.EnsureKeyUnique(ctx, baseKey)
	if err != nil {
		return nil, err
	}

	encrypted, err := m.crypto.Encrypt(req.Config)
	if err != nil {
		return nil, err
	}

	config := &models.SystemConfig{
		Category:    req.Category,
		Name:        req.Name,
		Key:         key,
		ConfigJSON:  encrypted,
		Description: req.Description,
		CreatedBy:   userID,
	}

	if req.IsEnabled != nil {
		config.IsEnabled = *req.IsEnabled
	}
	if req.IsDefault != nil {
		config.IsDefault = *req.IsDefault
	}
	if req.Priority != nil {
		config.Priority = *req.Priority
	}

	if err := m.repo.Create(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}

	m.cache.Invalidate(req.Category)
	m.eventBus.Publish(EventConfigCreated, config)

	return m.ToResponse(ctx, config)
}

// UpdateConfig 更新配置
func (m *Manager) UpdateConfig(ctx context.Context, id uint, req *models.SystemConfigStoreRequest) (*models.ConfigResponse, error) {
	config, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("config not found: %w", err)
	}

	if err := m.mergeConfig(config, req.Config); err != nil {
		return nil, err
	}

	config.Name = req.Name
	config.Description = req.Description
	if req.IsEnabled != nil {
		config.IsEnabled = *req.IsEnabled
	}
	if req.Priority != nil {
		config.Priority = *req.Priority
	}

	if err := m.repo.Update(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to update config: %w", err)
	}

	m.cache.Invalidate(config.Category)
	m.eventBus.Publish(EventConfigUpdated, config)

	return m.ToResponse(ctx, config)
}

// mergeConfig 合并配置
func (m *Manager) mergeConfig(config *models.SystemConfig, newConfig map[string]interface{}) error {
	existingConfig, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return fmt.Errorf("failed to decrypt existing config: %w", err)
	}

	for key, value := range newConfig {
		strValue, ok := value.(string)
		if ok && strValue == "******" {
			continue
		}
		existingConfig[key] = value
	}

	encrypted, err := m.crypto.Encrypt(existingConfig)
	if err != nil {
		return err
	}
	config.ConfigJSON = encrypted

	return nil
}

// DeleteConfig 删除配置
func (m *Manager) DeleteConfig(ctx context.Context, id uint) error {
	config, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := m.repo.Delete(ctx, id); err != nil {
		return err
	}

	m.cache.Invalidate(config.Category)
	m.eventBus.Publish(EventConfigDeleted, config)

	return nil
}

// GetConfig 获取配置
func (m *Manager) GetConfig(ctx context.Context, id uint, maskSensitive bool) (*models.ConfigResponse, error) {
	config, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return m.ToResponseWithMask(ctx, config, maskSensitive)
}

// ListConfigs 列出配置
func (m *Manager) ListConfigs(ctx context.Context, category models.ConfigCategory, enabledOnly, maskSensitive bool) ([]*models.ConfigResponse, error) {
	configs, err := m.repo.List(ctx, category, enabledOnly)
	if err != nil {
		return nil, err
	}

	responses := make([]*models.ConfigResponse, 0, len(configs))
	for _, config := range configs {
		resp, err := m.ToResponseWithMask(ctx, &config, maskSensitive)
		if err != nil {
			log.Printf("[ConfigManager] Failed to decrypt config ID=%d, Key=%s: %v", config.ID, config.Key, err)
			continue
		}
		responses = append(responses, resp)
	}

	return responses, nil
}

// ToResponse 转换为响应
func (m *Manager) ToResponse(ctx context.Context, config *models.SystemConfig) (*models.ConfigResponse, error) {
	return m.ToResponseWithMask(ctx, config, false)
}

// ToResponseWithMask 转换为响应（带脱敏）
func (m *Manager) ToResponseWithMask(ctx context.Context, config *models.SystemConfig, maskSensitive bool) (*models.ConfigResponse, error) {
	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil, err
	}

	if maskSensitive {
		configMap = MaskSensitiveData(configMap)
	}

	return &models.ConfigResponse{
		ID:          config.ID,
		Category:    config.Category,
		Name:        config.Name,
		Key:         config.Key,
		IsEnabled:   config.IsEnabled,
		IsDefault:   config.IsDefault,
		Priority:    config.Priority,
		Config:      configMap,
		Description: config.Description,
		CreatedBy:   config.CreatedBy,
		CreatedAt:   config.CreatedAt,
		UpdatedAt:   config.UpdatedAt,
	}, nil
}

// SetDefault 设置默认配置
func (m *Manager) SetDefault(ctx context.Context, id uint) error {
	config, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := m.repo.SetDefault(ctx, id, config.Category); err != nil {
		return err
	}

	m.cache.Invalidate(config.Category)
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// Enable 启用配置
func (m *Manager) Enable(ctx context.Context, id uint) error {
	if err := m.repo.Enable(ctx, id); err != nil {
		return err
	}

	config, _ := m.repo.GetByID(ctx, id)
	if config != nil {
		m.cache.Invalidate(config.Category)
	}
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// Disable 禁用配置
func (m *Manager) Disable(ctx context.Context, id uint) error {
	if err := m.repo.Disable(ctx, id); err != nil {
		return err
	}

	config, _ := m.repo.GetByID(ctx, id)
	if config != nil {
		m.cache.Invalidate(config.Category)
	}
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// GetJWTConfig 获取 JWT 配置
func (m *Manager) GetJWTConfig(ctx context.Context) (*JWTConfig, error) {
	// 先从缓存获取
	if cfg := m.cache.GetJWT(); cfg != nil {
		return cfg, nil
	}

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := m.EnsureDefaultJWTConfig(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default JWT config: %w", err)
			}
			config, err = m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
			if err != nil {
				return nil, fmt.Errorf("failed to get JWT config after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get JWT config: %w", err)
		}
	}

	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt JWT config: %w", err)
	}

	jwtConfig := &JWTConfig{
		Secret:          getStringFromMap(configMap, "secret", ""),
		AccessTokenTTL:  getStringFromMap(configMap, "access_token_ttl", "15m"),
		RefreshTokenTTL: getStringFromMap(configMap, "refresh_token_ttl", "168h"),
	}

	m.cache.SetJWT(jwtConfig)
	return jwtConfig, nil
}

// EnsureDefaultJWTConfig 确保默认 JWT 配置存在
func (m *Manager) EnsureDefaultJWTConfig(ctx context.Context) error {
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	secret := cryptoutils.GenerateRandomKey(32)

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryJWT,
		Name:     "JWT Settings",
		Config: map[string]interface{}{
			"secret":            secret,
			"access_token_ttl":  "15m",
			"refresh_token_ttl": "168h",
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "JWT authentication configuration",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default JWT config: %w", err)
	}

	log.Println("[ConfigManager] Default JWT config created successfully")
	return nil
}

// UpdateJWTConfig 更新 JWT 配置
func (m *Manager) UpdateJWTConfig(ctx context.Context, jwtConfig *JWTConfig) error {
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return m.EnsureDefaultJWTConfig(ctx)
		}
		return fmt.Errorf("failed to get JWT config: %w", err)
	}

	req := &models.SystemConfigStoreRequest{
		Category: config.Category,
		Name:     config.Name,
		Config: map[string]interface{}{
			"secret":            jwtConfig.Secret,
			"access_token_ttl":  jwtConfig.AccessTokenTTL,
			"refresh_token_ttl": jwtConfig.RefreshTokenTTL,
		},
		IsEnabled:   BoolPtr(true),
		Description: config.Description,
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}

// GetStorageConfigs 获取存储配置
func (m *Manager) GetStorageConfigs(ctx context.Context) ([]storage.StorageConfig, error) {
	// 先从缓存获取
	if cached := m.cache.GetStorage(); cached != nil {
		return cached, nil
	}

	configs, err := m.repo.List(ctx, models.ConfigCategoryStorage, true)
	if err != nil {
		return nil, fmt.Errorf("failed to list storage configs: %w", err)
	}

	result := make([]storage.StorageConfig, 0, len(configs))
	for _, cfg := range configs {
		configMap, err := m.crypto.Decrypt(cfg.ConfigJSON)
		if err != nil {
			log.Printf("[ConfigManager] Failed to decrypt storage config ID=%d: %v", cfg.ID, err)
			continue
		}

		storageCfg := storage.StorageConfig{
			ID:        cfg.ID,
			Name:      cfg.Name,
			IsDefault: cfg.IsDefault,
		}

		storageType := getStringFromMap(configMap, "type", "local")
		storageCfg.Type = storageType

		switch storageType {
		case "local":
			storageCfg.LocalPath = getStringFromMap(configMap, "local_path", "./data/upload")
		case "minio":
			storageCfg.Endpoint = getStringFromMap(configMap, "endpoint", "")
			storageCfg.AccessKeyID = getStringFromMap(configMap, "access_key_id", "")
			storageCfg.SecretAccessKey = getStringFromMap(configMap, "secret_access_key", "")
			storageCfg.BucketName = getStringFromMap(configMap, "bucket_name", "")
			if val, ok := configMap["use_ssl"]; ok {
				switch v := val.(type) {
				case bool:
					storageCfg.UseSSL = v
				case string:
					storageCfg.UseSSL = v == "true" || v == "1" || v == "yes"
				}
			} else {
				storageCfg.UseSSL = true
			}
		case "webdav":
			storageCfg.WebDAVURL = getStringFromMap(configMap, "webdav_url", "")
			storageCfg.WebDAVUsername = getStringFromMap(configMap, "webdav_username", "")
			storageCfg.WebDAVPassword = getStringFromMap(configMap, "webdav_password", "")
			storageCfg.WebDAVRootPath = getStringFromMap(configMap, "webdav_root_path", "")
		}

		result = append(result, storageCfg)
	}

	m.cache.SetStorage(result)
	return result, nil
}

// GetDefaultStorageConfigID 获取默认存储配置 ID
func (m *Manager) GetDefaultStorageConfigID(ctx context.Context) (uint, error) {
	configs, err := m.GetStorageConfigs(ctx)
	if err != nil {
		return 0, err
	}

	for _, cfg := range configs {
		if cfg.IsDefault {
			return cfg.ID, nil
		}
	}

	if len(configs) > 0 {
		return configs[0].ID, nil
	}

	return 0, fmt.Errorf("no storage config available")
}

// ensureDefaultLocalStorageConfig 确保存在默认本地存储配置
func (m *Manager) ensureDefaultLocalStorageConfig(ctx context.Context) error {
	configs, err := m.repo.List(ctx, models.ConfigCategoryStorage, true)
	if err != nil {
		return err
	}

	hasLocal := false
	for _, cfg := range configs {
		configMap, err := m.crypto.Decrypt(cfg.ConfigJSON)
		if err != nil {
			continue
		}
		if configType, ok := configMap["type"].(string); ok && configType == "local" {
			hasLocal = true
			break
		}
	}

	if hasLocal {
		return nil
	}

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Name:     "Local Storage",
		Config: map[string]interface{}{
			"type":       "local",
			"local_path": "./data/upload",
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(len(configs) == 0),
		Description: "Default local file storage",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default local storage config: %w", err)
	}

	log.Println("[ConfigManager] Default local storage config created successfully")
	return nil
}
