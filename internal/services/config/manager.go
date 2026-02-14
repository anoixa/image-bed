package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/configs"
	cryptoutils "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// Manager 配置管理器
type Manager struct {
	db         *gorm.DB
	repo       configs.Repository
	keyManager *cryptoutils.MasterKeyManager
	encryptor  *cryptoutils.ConfigEncryptor
	eventBus   *EventBus
	dataPath   string
}

// NewManager 创建配置管理器
func NewManager(db *gorm.DB, dataPath string) *Manager {
	return &Manager{
		db:         db,
		repo:       configs.NewRepository(db),
		keyManager: cryptoutils.NewMasterKeyManager(dataPath),
		eventBus:   NewEventBus(),
		dataPath:   dataPath,
	}
}

// Initialize 初始化配置系统
// 1. 初始化主密钥
// 2. 验证/创建 Canary
func (m *Manager) Initialize() error {
	// 1. 初始化主密钥
	checkDataExists := func() (bool, error) {
		count, err := m.repo.Count(context.Background())
		return count > 0, err
	}

	if err := m.keyManager.Initialize(checkDataExists); err != nil {
		return fmt.Errorf("failed to initialize master key: %w", err)
	}

	// 2. 创建加密器
	m.encryptor = cryptoutils.NewConfigEncryptor(m.keyManager.GetKey())

	// 3. 验证/创建 Canary
	if err := m.ensureCanary(); err != nil {
		return fmt.Errorf("failed to ensure canary: %w", err)
	}

	log.Println("[ConfigManager] Initialized successfully")
	return nil
}

// ensureCanary 确保 Canary 记录存在且可解密
func (m *Manager) ensureCanary() error {
	ctx := context.Background()

	// 尝试获取 Canary
	canary, err := m.repo.GetByKey(ctx, "system:encryption_canary")
	if err != nil {
		// 不存在，创建新的 Canary
		if err == gorm.ErrRecordNotFound {
			return m.createCanary(ctx)
		}
		return err
	}

	// 存在，验证能否解密
	_, err = m.encryptor.Decrypt(canary.ConfigJSON)
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
	encrypted := m.encryptor.Encrypt(string(jsonData))

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
	jsonData, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	encrypted := m.encryptor.Encrypt(string(jsonData))
	return encrypted, nil
}

// DecryptConfig 解密配置
func (m *Manager) DecryptConfig(encrypted string) (map[string]interface{}, error) {
	decrypted, err := m.encryptor.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
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

// GetEncryptor 获取加密器（用于其他服务）
func (m *Manager) GetEncryptor() *cryptoutils.ConfigEncryptor {
	return m.encryptor
}

// GetRepo 获取仓库（用于其他服务）
func (m *Manager) GetRepo() configs.Repository {
	return m.repo
}
