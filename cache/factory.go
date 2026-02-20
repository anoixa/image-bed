package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/cache/memory"
	"github.com/anoixa/image-bed/cache/redis"
	"github.com/anoixa/image-bed/config"
)

// providers 存储所有配置的缓存提供者
var providers = make(map[uint]Provider)
var defaultProvider Provider
var defaultID uint

// CacheConfig 缓存配置
// 用于从配置源（数据库/文件）读取配置后传递给 InitCache
type CacheConfig struct {
	ID       uint
	Name     string
	Type     string // "memory" or "redis"
	IsDefault bool
	// Memory
	NumCounters int64
	MaxCost     int64
	BufferItems int64
	Metrics     bool
	// Redis
	Address      string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
}

// InitCache 初始化缓存层
// 在应用启动时调用，配置从数据库或其他配置源读取
func InitCache(configs []CacheConfig) error {
	for _, cfg := range configs {
		provider, err := createProvider(cfg)
		if err != nil {
			return fmt.Errorf("failed to create cache %s: %w", cfg.Name, err)
		}
		providers[cfg.ID] = provider
		if cfg.IsDefault {
			defaultProvider = provider
			defaultID = cfg.ID
		}
	}

	// 如果没有配置，使用默认内存缓存
	if defaultProvider == nil {
		provider, err := memory.NewMemory(memory.Config{
			NumCounters: 1000000,
			MaxCost:     1073741824, // 1GB
			BufferItems: 64,
			Metrics:     true,
		})
		if err != nil {
			return fmt.Errorf("failed to create default memory cache: %w", err)
		}
		providers[0] = provider
		defaultProvider = provider
		defaultID = 0
	}

	return nil
}

// GetDefault 获取默认缓存提供者
func GetDefault() Provider {
	return defaultProvider
}

// GetDefaultID 获取默认缓存配置ID
func GetDefaultID() uint {
	return defaultID
}

// GetByID 按ID获取缓存提供者
func GetByID(id uint) (Provider, error) {
	provider, ok := providers[id]
	if !ok {
		return nil, fmt.Errorf("cache provider with ID %d not found", id)
	}
	return provider, nil
}

func createProvider(cfg CacheConfig) (Provider, error) {
	switch cfg.Type {
	case "memory":
		memConfig := memory.Config{
			NumCounters: cfg.NumCounters,
			MaxCost:     cfg.MaxCost,
			BufferItems: cfg.BufferItems,
			Metrics:     cfg.Metrics,
		}
		// 使用默认值
		if memConfig.NumCounters == 0 {
			memConfig.NumCounters = 1000000
		}
		if memConfig.MaxCost == 0 {
			memConfig.MaxCost = 1073741824
		}
		if memConfig.BufferItems == 0 {
			memConfig.BufferItems = 64
		}
		return memory.NewMemory(memConfig)
	case "redis":
		return redis.NewRedisFromConfig(&redis.Config{
			Address:      cfg.Address,
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
		})
	default:
		return nil, fmt.Errorf("unsupported cache type: %s", cfg.Type)
	}
}

// InitFromConfig 从静态配置初始化缓存
func InitFromConfig(cfg *config.Config) error {
	var cacheCfg CacheConfig

	switch cfg.CacheType {
	case "redis":
		cacheCfg = CacheConfig{
			ID:        0,
			Name:      "default-redis",
			Type:      "redis",
			IsDefault: true,
			Address:   cfg.CacheRedisAddr,
			Password:  cfg.CacheRedisPassword,
			DB:        cfg.CacheRedisDB,
		}
	case "memory", "":
		// 默认使用内存缓存
		cacheCfg = CacheConfig{
			ID:          0,
			Name:        "default-memory",
			Type:        "memory",
			IsDefault:   true,
			NumCounters: 1000000,
			MaxCost:     1073741824, // 1GB
			BufferItems: 64,
			Metrics:     true,
		}
	default:
		return fmt.Errorf("unsupported cache type from config: %s", cfg.CacheType)
	}

	return InitCache([]CacheConfig{cacheCfg})
}

// Factory 缓存工厂（兼容旧测试）
type Factory struct {
	providers       map[uint]Provider
	defaultProvider Provider
}

// NewFactory 创建新的缓存工厂
func NewFactory() *Factory {
	return &Factory{
		providers: make(map[uint]Provider),
	}
}

// SetProvider 设置提供者
func (f *Factory) SetProvider(id uint, provider Provider) {
	if f.providers == nil {
		f.providers = make(map[uint]Provider)
	}
	f.providers[id] = provider
}

// SetDefaultProvider 设置默认提供者
func (f *Factory) SetDefaultProvider(provider Provider) {
	f.defaultProvider = provider
}

// GetProvider 获取默认提供者
func (f *Factory) GetProvider() Provider {
	if f.defaultProvider != nil {
		return f.defaultProvider
	}
	return GetDefault()
}

// Set 设置缓存值
func (f *Factory) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	provider := f.GetProvider()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return provider.Set(ctx, key, value, expiration)
}

// Get 获取缓存值
func (f *Factory) Get(ctx context.Context, key string, dest interface{}) error {
	provider := f.GetProvider()
	if provider == nil {
		return ErrCacheMiss
	}
	return provider.Get(ctx, key, dest)
}

// Delete 删除缓存
func (f *Factory) Delete(ctx context.Context, key string) error {
	provider := f.GetProvider()
	if provider == nil {
		return nil 
	}
	return provider.Delete(ctx, key)
}

// Exists 检查缓存是否存在
func (f *Factory) Exists(ctx context.Context, key string) (bool, error) {
	provider := f.GetProvider()
	if provider == nil {
		return false, fmt.Errorf("cache provider not initialized")
	}
	return provider.Exists(ctx, key)
}

// Close 关闭缓存
func (f *Factory) Close() error {
	if f.defaultProvider != nil {
		return f.defaultProvider.Close()
	}
	return nil
}

// Name 返回缓存工厂名称（实现Provider接口）
func (f *Factory) Name() string {
	if f.defaultProvider != nil {
		return f.defaultProvider.Name()
	}
	return "factory"
}

// 确保 Factory 实现了 Provider 接口
var _ Provider = (*Factory)(nil)
