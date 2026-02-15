package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	configSvc "github.com/anoixa/image-bed/internal/services/config"
	"gorm.io/gorm"
)

// Factory 存储工厂 - 支持多存储后端、数据库配置和热重载
type Factory struct {
	providers       map[uint]Provider   // key: config ID，支持多后端（如 minio1, minio2）
	providersByName map[string]Provider // key: config name
	defaultProvider Provider
	defaultID       uint
	defaultName     string

	mu sync.RWMutex // 保护上述字段

	db        *gorm.DB
	encryptor interface{ Decrypt(string) (string, error) }
}

// NewFactory 创建存储工厂
func NewFactory(db *gorm.DB, manager *configSvc.Manager) (*Factory, error) {
	factory := &Factory{
		providers:       make(map[uint]Provider),
		providersByName: make(map[string]Provider),
		db:              db,
	}

	if manager != nil {
		factory.encryptor = manager.GetEncryptor()
	}

	if db != nil {
		if err := factory.LoadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load storage configs: %w", err)
		}
	}

	if len(factory.providers) == 0 {
		return nil, fmt.Errorf("no storage providers were successfully initialized")
	}

	return factory, nil
}

// LoadFromDB 从数据库加载存储配置
func (f *Factory) LoadFromDB() error {
	if f.db == nil {
		return fmt.Errorf("database is nil")
	}

	log.Println("[StorageFactory] Loading storage providers from database...")

	var configs []models.SystemConfig
	if err := f.db.Where("category = ? AND is_enabled = ?", models.ConfigCategoryStorage, true).Find(&configs).Error; err != nil {
		return fmt.Errorf("failed to query storage configs: %w", err)
	}

	for _, cfg := range configs {
		if err := f.loadProvider(&cfg); err != nil {
			log.Printf("[StorageFactory] Failed to load storage config %d (%s): %v", cfg.ID, cfg.Name, err)
			continue
		}
	}

	log.Printf("[StorageFactory] Loaded %d storage providers from database", len(f.providers))
	return nil
}

// loadProvider 从配置记录加载 provider
func (f *Factory) loadProvider(cfg *models.SystemConfig) error {
	// 解密配置
	var configJSON string
	var err error
	if f.encryptor != nil {
		configJSON, err = f.encryptor.Decrypt(cfg.ConfigJSON)
		if err != nil {
			return fmt.Errorf("failed to decrypt config: %w", err)
		}
	} else {
		configJSON = cfg.ConfigJSON
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	storageType, _ := configMap["type"].(string)
	if storageType == "" {
		return fmt.Errorf("storage type not found")
	}

	var provider Provider
	switch storageType {
	case "local":
		path, _ := configMap["local_path"].(string)
		if path == "" {
			return fmt.Errorf("local_path is required")
		}
		p, err := NewLocalStorage(path)
		if err != nil {
			return fmt.Errorf("failed to create local storage: %w", err)
		}
		provider = p

	case "minio":
		minioCfg := config.MinioConfig{
			Endpoint:        getString(configMap, "endpoint"),
			AccessKeyID:     getString(configMap, "access_key_id"),
			SecretAccessKey: getString(configMap, "secret_access_key"),
			UseSSL:          getBool(configMap, "use_ssl"),
			BucketName:      getString(configMap, "bucket_name"),
		}
		p, err := NewMinioStorage(minioCfg)
		if err != nil {
			return fmt.Errorf("failed to create minio storage: %w", err)
		}
		provider = p

	default:
		return fmt.Errorf("unsupported storage type: %s", storageType)
	}

	// 健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := provider.Health(ctx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// 添加到 maps（支持多后端）
	f.providers[cfg.ID] = provider
	f.providersByName[cfg.Name] = provider

	if cfg.IsDefault {
		f.defaultProvider = provider
		f.defaultID = cfg.ID
		f.defaultName = cfg.Name
	}

	return nil
}

// ReloadConfig 热重载指定配置
func (f *Factory) ReloadConfig(configID uint) error {
	var cfg models.SystemConfig
	if err := f.db.First(&cfg, configID).Error; err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	if cfg.Category != models.ConfigCategoryStorage {
		return fmt.Errorf("not a storage config")
	}

	// 如果禁用，移除
	if !cfg.IsEnabled {
		f.mu.Lock()
		delete(f.providers, cfg.ID)
		delete(f.providersByName, cfg.Name)
		f.mu.Unlock()
		return nil
	}

	// 创建新 provider
	newProvider, err := f.createProvider(&cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := newProvider.Health(ctx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// 原子替换
	f.mu.Lock()
	oldProvider := f.providers[configID]
	f.providers[configID] = newProvider
	f.providersByName[cfg.Name] = newProvider

	if cfg.IsDefault {
		f.defaultProvider = newProvider
		f.defaultID = configID
		f.defaultName = cfg.Name
	}
	f.mu.Unlock()

	// 优雅关闭旧 provider
	if oldProvider != nil {
		go func() {
			time.Sleep(30 * time.Second)
			if closer, ok := oldProvider.(interface{ Close() error }); ok {
				closer.Close()
			}
		}()
	}

	return nil
}

// createProvider 从配置创建 provider
func (f *Factory) createProvider(cfg *models.SystemConfig) (Provider, error) {
	var configJSON string
	var err error
	if f.encryptor != nil {
		configJSON, err = f.encryptor.Decrypt(cfg.ConfigJSON)
		if err != nil {
			return nil, err
		}
	} else {
		configJSON = cfg.ConfigJSON
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
		return nil, err
	}

	storageType, _ := configMap["type"].(string)
	switch storageType {
	case "local":
		path, _ := configMap["local_path"].(string)
		return NewLocalStorage(path)
	case "minio":
		minioCfg := config.MinioConfig{
			Endpoint:        getString(configMap, "endpoint"),
			AccessKeyID:     getString(configMap, "access_key_id"),
			SecretAccessKey: getString(configMap, "secret_access_key"),
			UseSSL:          getBool(configMap, "use_ssl"),
			BucketName:      getString(configMap, "bucket_name"),
		}
		return NewMinioStorage(minioCfg)
	default:
		return nil, fmt.Errorf("unsupported type: %s", storageType)
	}
}

// GetByID 按配置 ID 获取 provider
func (f *Factory) GetByID(id uint) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	provider, ok := f.providers[id]
	if !ok {
		return nil, fmt.Errorf("storage provider with ID %d not found", id)
	}
	return provider, nil
}

// GetByName 按名称获取 provider
func (f *Factory) GetByName(name string) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if name == "" {
		if f.defaultProvider == nil {
			return nil, fmt.Errorf("no default storage provider")
		}
		return f.defaultProvider, nil
	}

	provider, ok := f.providersByName[name]
	if !ok {
		return nil, fmt.Errorf("storage provider '%s' not found", name)
	}
	return provider, nil
}

// GetDefault 获取默认 provider
func (f *Factory) GetDefault() Provider {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.defaultProvider
}

// GetDefaultID 获取默认 provider ID
func (f *Factory) GetDefaultID() uint {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.defaultID
}

// GetDefaultName 获取默认 provider 名称
func (f *Factory) GetDefaultName() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.defaultName
}

// GetIDByName 按名称获取 provider ID
func (f *Factory) GetIDByName(name string) (uint, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if name == "" {
		if f.defaultProvider == nil {
			return 0, fmt.Errorf("no default storage provider")
		}
		return f.defaultID, nil
	}

	for id, provider := range f.providers {
		if provider.Name() == name {
			return id, nil
		}
	}

	return 0, fmt.Errorf("storage provider '%s' not found", name)
}

// List 列出所有 provider 名称
func (f *Factory) List() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	names := make([]string, 0, len(f.providersByName))
	for name := range f.providersByName {
		names = append(names, name)
	}
	return names
}

// ListInfo 列出所有 provider 详细信息
func (f *Factory) ListInfo() []map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(f.providers))
	for id, provider := range f.providers {
		info := map[string]interface{}{
			"id":   id,
			"name": provider.Name(),
		}
		if id == f.defaultID {
			info["is_default"] = true
		}
		result = append(result, info)
	}
	return result
}

// Get 获取 provider（向后兼容，使用 GetByName）
func (f *Factory) Get(name string) (Provider, error) {
return f.GetByName(name)
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
