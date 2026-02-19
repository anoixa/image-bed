package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/configs"
	cryptoservice "github.com/anoixa/image-bed/internal/services/crypto"
	cryptoutils "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// Manager 配置管理器
type Manager struct {
	db        *gorm.DB
	repo      configs.Repository
	crypto    *cryptoservice.Service
	eventBus  *EventBus
	dataPath  string
	cache     cache.Provider
	cacheTTL  time.Duration
}

// JWTConfig JWT 配置结构
type JWTConfig struct {
	Secret           string
	AccessTokenTTL   string
	RefreshTokenTTL  string
}

// NewManager 创建配置管理器
func NewManager(db *gorm.DB, dataPath string) *Manager {
	return &Manager{
		db:       db,
		repo:     configs.NewRepository(db),
		crypto:   cryptoservice.NewService(dataPath),
		eventBus: NewEventBus(),
		dataPath: dataPath,
		cacheTTL: configs.DefaultCacheTTL,
	}
}

// NewManagerWithCache 创建带缓存的配置管理器
// 简化版本：不再使用装饰器模式，缓存逻辑由调用者处理
func NewManagerWithCache(db *gorm.DB, dataPath string, cacheProvider cache.Provider, cacheTTL time.Duration) *Manager {
	if cacheTTL == 0 {
		cacheTTL = configs.DefaultCacheTTL
	}

	return &Manager{
		db:       db,
		repo:     configs.NewRepository(db),
		crypto:   cryptoservice.NewService(dataPath),
		eventBus: NewEventBus(),
		dataPath: dataPath,
		cache:    cacheProvider,
		cacheTTL: cacheTTL,
	}
}

// SetCache 设置缓存（简化版本，仅存储缓存提供者供外部使用）
func (m *Manager) SetCache(cacheProvider cache.Provider, cacheTTL time.Duration) {
	if cacheProvider == nil {
		return
	}

	m.cache = cacheProvider
	if cacheTTL > 0 {
		m.cacheTTL = cacheTTL
	}

	log.Println("[ConfigManager] Cache enabled with TTL:", m.cacheTTL)
}

// GetCache 获取缓存提供者
func (m *Manager) GetCache() cache.Provider {
	return m.cache
}

// Initialize 初始化配置
func (m *Manager) Initialize() error {
	checkDataExists := func() (bool, error) {
		count, err := m.repo.Count(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "no such table") ||
				errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		return count > 0, nil
	}

	if err := m.crypto.Initialize(checkDataExists); err != nil {
		return fmt.Errorf("failed to initialize crypto service: %w", err)
	}

	// 验证/创建 Canary
	if err := m.ensureCanary(); err != nil {
		return fmt.Errorf("failed to ensure canary: %w", err)
	}

	// 创建默认转换配置（如果不存在）
	if err := m.ensureDefaultConversionConfig(); err != nil {
		return fmt.Errorf("failed to ensure conversion config: %w", err)
	}

	// 创建默认缩略图配置（如果不存在）
	ctx := context.Background()
	if err := m.ensureDefaultThumbnailConfig(ctx); err != nil {
		return fmt.Errorf("failed to ensure thumbnail config: %w", err)
	}

	log.Println("[ConfigManager] Initialized successfully")
	return nil
}

// ensureDefaultConversionConfig 确保存在默认的图片转换配置
func (m *Manager) ensureDefaultConversionConfig() error {
	ctx := context.Background()

	// 检查是否已有转换配置
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryConversion)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil // 表不存在，跳过
		}
		return err
	}

	// 如果已有转换配置，跳过
	if count > 0 {
		return nil
	}

	// 创建默认配置
	configData := map[string]interface{}{
		"enabled_formats":     []string{"webp"},
		"webp_quality":        85,
		"avif_quality":        80,
		"avif_effort":         4,
		"max_dimension":       0,
		"skip_smaller_than":   0,
		"max_retries":         3,
		"retry_base_interval": 300,
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryConversion,
		Name:        "Image Conversion Settings",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Default image format conversion settings (WebP enabled by default)",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigManager] Default conversion config created (WebP enabled)")
	return nil
}

func (m *Manager) ensureCanary() error {
	ctx := context.Background()

	canary, err := m.repo.GetByKey(ctx, "system:encryption_canary")
	if err != nil {
		if err == gorm.ErrRecordNotFound ||
			strings.Contains(err.Error(), "no such table") {
			return m.createCanary(ctx)
		}
		return err
	}

	// 存在，验证能否解密
	_, err = m.crypto.DecryptString(canary.ConfigJSON)
	if err != nil {
		return fmt.Errorf("failed to decrypt canary, master key may be incorrect: %w", err)
	}

	log.Println("[ConfigManager] Canary verified successfully")
	return nil
}

// createCanary 创建 Canary 记录
func (m *Manager) createCanary(ctx context.Context) error {
	canaryData := map[string]string{
		"check":       "ok",
		"version":     "1",
		"description": "Encryption verification canary",
	}

	jsonData, _ := json.Marshal(canaryData)
	encrypted := m.crypto.EncryptString(string(jsonData))

	canary := &models.SystemConfig{
		Category:    models.ConfigCategorySystem,
		Name:        "Encryption Canary",
		Key:         "system:encryption_canary",
		IsEnabled:   true,
		IsDefault:   false,
		ConfigJSON:  encrypted,
		Description: "用于验证加密密钥正确性的内部配置",
	}

	if err := m.repo.Create(ctx, canary); err != nil {
		return err
	}

	log.Println("[ConfigManager] Canary created successfully")
	return nil
}

// EncryptConfig 加密配置
func (m *Manager) EncryptConfig(config map[string]interface{}) (string, error) {
	return m.crypto.EncryptJSON(config)
}

// DecryptConfig 解密配置
func (m *Manager) DecryptConfig(encrypted string) (map[string]interface{}, error) {
	return m.crypto.DecryptJSON(encrypted)
}

// CreateConfig 创建配置
func (m *Manager) CreateConfig(ctx context.Context, req *models.SystemConfigStoreRequest, userID uint) (*models.ConfigResponse, error) {
	// 生成唯一 Key
	baseKey := fmt.Sprintf("%s:%s", req.Category, req.Name)
	key, err := m.repo.EnsureKeyUnique(ctx, baseKey)
	if err != nil {
		return nil, err
	}

	// 加密配置
	encrypted, err := m.EncryptConfig(req.Config)
	if err != nil {
		return nil, err
	}

	// 创建配置
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

	// 触发事件
	m.eventBus.Publish(EventConfigCreated, config)

	return m.ToResponse(ctx, config)
}

// UpdateConfig 更新配置
func (m *Manager) UpdateConfig(ctx context.Context, id uint, req *models.SystemConfigStoreRequest) (*models.ConfigResponse, error) {
	// 获取现有配置
	config, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("config not found: %w", err)
	}

	// 处理配置更新（敏感字段保留）
	if err := m.mergeConfig(ctx, config, req.Config); err != nil {
		return nil, err
	}

	// 更新其他字段
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

	// 触发事件
	m.eventBus.Publish(EventConfigUpdated, config)

	return m.ToResponse(ctx, config)
}

// mergeConfig 合并配置（处理敏感字段）
func (m *Manager) mergeConfig(ctx context.Context, config *models.SystemConfig, newConfig map[string]interface{}) error {
	// 解密现有配置
	existingConfig, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		return fmt.Errorf("failed to decrypt existing config: %w", err)
	}

	// 合并配置（保留敏感字段）
	for key, value := range newConfig {
		strValue, ok := value.(string)
		if ok && strValue == "******" {
			// 敏感字段保留原值
			continue
		}
		existingConfig[key] = value
	}

	// 重新加密
	encrypted, err := m.EncryptConfig(existingConfig)
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

	// 触发事件
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
			continue
		}
		responses = append(responses, resp)
	}

	return responses, nil
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

	// 触发事件
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// Enable 启用配置
func (m *Manager) Enable(ctx context.Context, id uint) error {
	if err := m.repo.Enable(ctx, id); err != nil {
		return err
	}

	config, _ := m.repo.GetByID(ctx, id)
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// Disable 禁用配置
func (m *Manager) Disable(ctx context.Context, id uint) error {
	if err := m.repo.Disable(ctx, id); err != nil {
		return err
	}

	config, _ := m.repo.GetByID(ctx, id)
	m.eventBus.Publish(EventConfigUpdated, config)

	return nil
}

// ToResponse 转换为响应（不脱敏）
func (m *Manager) ToResponse(ctx context.Context, config *models.SystemConfig) (*models.ConfigResponse, error) {
	return m.ToResponseWithMask(ctx, config, false)
}

// ToResponseWithMask 转换为响应（可选择脱敏）
func (m *Manager) ToResponseWithMask(ctx context.Context, config *models.SystemConfig, maskSensitive bool) (*models.ConfigResponse, error) {
	// 解密配置
	configMap, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		return nil, err
	}

	// 脱敏处理
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

// Subscribe 订阅配置变更事件
func (m *Manager) Subscribe(eventType EventType, handler EventHandler) {
	m.eventBus.Subscribe(eventType, handler)
}

// MaskSensitiveData 脱敏敏感数据
func MaskSensitiveData(config map[string]interface{}) map[string]interface{} {
	sensitiveFields := []string{
		"secret", "secret_access_key", "access_key_id", "password",
	}

	result := make(map[string]interface{})
	for k, v := range config {
		isSensitive := false
		for _, sf := range sensitiveFields {
			if strings.EqualFold(k, sf) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			result[k] = "******"
		} else {
			result[k] = v
		}
	}

	return result
}

// GetCrypto 获取加密服务（用于其他服务）
func (m *Manager) GetCrypto() *cryptoservice.Service {
	return m.crypto
}

// GetRepo 获取仓库（用于其他服务）
func (m *Manager) GetRepo() configs.Repository {
	return m.repo
}

// GetJWTConfig 获取 JWT 配置
// 如果不存在，会自动创建默认配置
func (m *Manager) GetJWTConfig(ctx context.Context) (*JWTConfig, error) {
	// 尝试获取默认 JWT 配置
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 不存在，创建默认配置
			if err := m.EnsureDefaultJWTConfig(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default JWT config: %w", err)
			}
			// 重新获取
			config, err = m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
			if err != nil {
				return nil, fmt.Errorf("failed to get JWT config after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get JWT config: %w", err)
		}
	}

	// 解密配置
	configMap, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt JWT config: %w", err)
	}

	return &JWTConfig{
		Secret:          getStringFromMap(configMap, "secret", ""),
		AccessTokenTTL:  getStringFromMap(configMap, "access_token_ttl", "15m"),
		RefreshTokenTTL: getStringFromMap(configMap, "refresh_token_ttl", "168h"),
	}, nil
}

// EnsureDefaultJWTConfig 确保默认 JWT 配置存在
func (m *Manager) EnsureDefaultJWTConfig(ctx context.Context) error {
	// 检查是否已存在 JWT 配置
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil // 已存在
	}

	// 生成随机密钥
	secret := cryptoutils.GenerateRandomKey(32)

	configData := map[string]interface{}{
		"secret":            secret,
		"access_token_ttl":  "15m",
		"refresh_token_ttl": "168h",
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryJWT,
		Name:        "JWT Settings",
		Config:      configData,
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
	// 获取现有配置
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 不存在，创建新配置
			return m.EnsureDefaultJWTConfig(ctx)
		}
		return fmt.Errorf("failed to get JWT config: %w", err)
	}

	// 构建新配置
	configData := map[string]interface{}{
		"secret":            jwtConfig.Secret,
		"access_token_ttl":  jwtConfig.AccessTokenTTL,
		"refresh_token_ttl": jwtConfig.RefreshTokenTTL,
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryJWT,
		Name:        config.Name,
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		Description: config.Description,
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	if err != nil {
		return fmt.Errorf("failed to update JWT config: %w", err)
	}

	return nil
}

// getStringFromMap 从 map 中获取字符串值，提供默认值
func getStringFromMap(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

// BoolPtr 返回 bool 指针
func BoolPtr(b bool) *bool {
	return &b
}
