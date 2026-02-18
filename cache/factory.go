package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anoixa/image-bed/cache/memory"
	"github.com/anoixa/image-bed/cache/redis"
	"github.com/anoixa/image-bed/database/models"
	cryptoservice "github.com/anoixa/image-bed/internal/services/crypto"
	"gorm.io/gorm"
)

// Factory 缓存
type Factory struct {
	providers       map[uint]Provider   // key: config ID，支持多后端
	providersByName map[string]Provider // key: config name
	defaultProvider Provider
	defaultID       uint
	defaultName     string

	mu sync.RWMutex // 保护上述字段

	db     *gorm.DB
	crypto *cryptoservice.Service
}

// NewFactory 创建缓存工厂
func NewFactory(db *gorm.DB, crypto *cryptoservice.Service) (*Factory, error) {
	factory := &Factory{
		providers:       make(map[uint]Provider),
		providersByName: make(map[string]Provider),
		db:              db,
		crypto:          crypto,
	}

	if db != nil {
		if err := factory.LoadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load cache configs: %w", err)
		}
	}

	if len(factory.providers) == 0 {
		// 如果没有从数据库加载到配置，使用内存缓存作为默认
		log.Println("[CacheFactory] No cache providers from DB, using default memory cache")
		memConfig := memory.Config{
			NumCounters: 1000000,
			MaxCost:     1073741824, // 1GB
			BufferItems: 64,
			Metrics:     true,
		}
		memProvider, err := memory.NewMemory(memConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create default memory cache: %w", err)
		}
		factory.defaultProvider = memProvider
		factory.defaultName = "memory"
	}

	return factory, nil
}

// LoadFromDB 从数据库加载缓存配置
func (f *Factory) LoadFromDB() error {
	if f.db == nil {
		return fmt.Errorf("database is nil")
	}

	log.Println("[CacheFactory] Loading cache providers from database...")

	var configs []models.SystemConfig
	if err := f.db.Where("category = ? AND is_enabled = ?", models.ConfigCategoryCache, true).Find(&configs).Error; err != nil {
		return fmt.Errorf("failed to query cache configs: %w", err)
	}

	for _, cfg := range configs {
		if err := f.loadProvider(&cfg); err != nil {
			log.Printf("[CacheFactory] Failed to load cache config %d (%s): %v", cfg.ID, cfg.Name, err)
			continue
		}
	}

	log.Printf("[CacheFactory] Loaded %d cache providers from database", len(f.providers))
	return nil
}

// loadProvider 从配置记录加载 provider
func (f *Factory) loadProvider(cfg *models.SystemConfig) error {
	// 解密配置
	var configJSON string
	var err error
	if f.crypto != nil {
		configJSON, err = f.crypto.DecryptString(cfg.ConfigJSON)
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

	providerType, _ := configMap["provider_type"].(string)
	if providerType == "" {
		providerType = "memory"
	}

	var provider Provider
	switch providerType {
	case "memory":
		provider, err = f.createMemoryProvider(configMap)
	case "redis":
		provider, err = f.createRedisProvider(configMap)
	default:
		return fmt.Errorf("unsupported cache provider type: %s", providerType)
	}

	if err != nil {
		return fmt.Errorf("failed to create %s provider: %w", providerType, err)
	}

	// 保存 provider
	f.mu.Lock()
	defer f.mu.Unlock()

	f.providers[cfg.ID] = provider
	f.providersByName[cfg.Key] = provider

	// 如果是默认配置，设置为默认
	if cfg.IsDefault {
		f.defaultProvider = provider
		f.defaultID = cfg.ID
		f.defaultName = cfg.Key
		log.Printf("[CacheFactory] Set default cache provider: %s (ID: %d)", cfg.Key, cfg.ID)
	}

	return nil
}

// createMemoryProvider 创建内存缓存提供者
func (f *Factory) createMemoryProvider(configMap map[string]interface{}) (Provider, error) {
	numCounters := int64(1000000)
	maxCost := int64(1073741824) // 1GB
	bufferItems := int64(64)
	metrics := true

	if nc, ok := configMap["num_counters"].(float64); ok {
		numCounters = int64(nc)
	}
	if mc, ok := configMap["max_cost"].(float64); ok {
		maxCost = int64(mc)
	}
	if bi, ok := configMap["buffer_items"].(float64); ok {
		bufferItems = int64(bi)
	}
	if m, ok := configMap["metrics"].(bool); ok {
		metrics = m
	}

	memConfig := memory.Config{
		NumCounters: numCounters,
		MaxCost:     maxCost,
		BufferItems: bufferItems,
		Metrics:     metrics,
	}

	return memory.NewMemory(memConfig)
}

// createRedisProvider 创建 Redis 缓存提供者
func (f *Factory) createRedisProvider(configMap map[string]interface{}) (Provider, error) {
	address := "localhost:6379"
	password := ""
	db := 0
	poolSize := 10
	minIdleConns := 5

	if addr, ok := configMap["address"].(string); ok && addr != "" {
		address = addr
	}
	if pwd, ok := configMap["password"].(string); ok {
		password = pwd
	}
	if d, ok := configMap["db"].(float64); ok {
		db = int(d)
	}
	if ps, ok := configMap["pool_size"].(float64); ok {
		poolSize = int(ps)
	}
	if mic, ok := configMap["min_idle_conns"].(float64); ok {
		minIdleConns = int(mic)
	}

	redisConfig := &redis.Config{
		Address:      address,
		Password:     password,
		DB:           db,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
	}

	return redis.NewRedisFromConfig(redisConfig)
}

// GetProvider 获取默认缓存提供者
func (f *Factory) GetProvider() Provider {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.defaultProvider
}

// GetByID 通过 ID 获取缓存提供者
func (f *Factory) GetByID(id uint) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if provider, ok := f.providers[id]; ok {
		return provider, nil
	}
	return nil, fmt.Errorf("cache provider with ID %d not found", id)
}

// GetByName 通过名称获取缓存提供者
func (f *Factory) GetByName(name string) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if provider, ok := f.providersByName[name]; ok {
		return provider, nil
	}
	return nil, fmt.Errorf("cache provider with name %s not found", name)
}

// ListInfo 列出所有缓存提供者信息
func (f *Factory) ListInfo() []map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var result []map[string]interface{}
	for id, provider := range f.providers {
		info := map[string]interface{}{
			"id":       id,
			"name":     provider.Name(),
			"is_default": id == f.defaultID,
		}
		result = append(result, info)
	}
	return result
}

// ReloadConfig 热重载指定缓存配置
func (f *Factory) ReloadConfig(configID uint) error {
	log.Printf("[CacheFactory] Reloading cache config %d...", configID)

	var cfg models.SystemConfig
	if err := f.db.First(&cfg, configID).Error; err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	if cfg.Category != models.ConfigCategoryCache {
		return fmt.Errorf("config %d is not a cache config", configID)
	}

	// 关闭旧 provider
	f.mu.Lock()
	if oldProvider, ok := f.providers[configID]; ok {
		if err := oldProvider.Close(); err != nil {
			log.Printf("[CacheFactory] Error closing old provider: %v", err)
		}
	}
	f.mu.Unlock()

	// 加载新 provider
	if err := f.loadProvider(&cfg); err != nil {
		return fmt.Errorf("failed to reload provider: %w", err)
	}

	log.Printf("[CacheFactory] Cache config %d reloaded successfully", configID)
	return nil
}

// Close 关闭所有缓存提供者
func (f *Factory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var lastErr error
	for id, provider := range f.providers {
		if err := provider.Close(); err != nil {
			log.Printf("[CacheFactory] Error closing provider %d: %v", id, err)
			lastErr = err
		}
	}

	return lastErr
}

// --- 便捷方法 ---

// Set 设置缓存项
func (f *Factory) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	provider := f.GetProvider()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return provider.Set(ctx, key, value, expiration)
}

// Get 获取缓存项
func (f *Factory) Get(ctx context.Context, key string, dest interface{}) error {
	provider := f.GetProvider()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return provider.Get(ctx, key, dest)
}

// Delete 删除缓存项
func (f *Factory) Delete(ctx context.Context, key string) error {
	provider := f.GetProvider()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return provider.Delete(ctx, key)
}

// Exists 检查缓存项是否存在
func (f *Factory) Exists(ctx context.Context, key string) (bool, error) {
	provider := f.GetProvider()
	if provider == nil {
		return false, fmt.Errorf("cache provider not initialized")
	}
	return provider.Exists(ctx, key)
}
