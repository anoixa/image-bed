package cache

import (
	"context"
	"fmt"
	"time"
)

// Provider 缓存提供者接口
type Provider interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string, dest interface{}) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
	Name() string
}

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = fmt.Errorf("cache miss")

// IsCacheMiss 判断是否为缓存未命中错误
func IsCacheMiss(err error) bool {
	return err == ErrCacheMiss
}

var defaultProvider Provider

// Config 缓存配置
type Config struct {
	Type        string // "memory" or "redis"
	NumCounters int64  // memory only
	MaxCost     int64  // memory only
	BufferItems int64  // memory only
	Metrics     bool   // memory only
	Address     string // redis only
	Password    string // redis only
	DB          int    // redis only
	PoolSize    int    // redis only
}

// Init 初始化缓存层 - 简化版本，只支持单实例
func Init(cfg Config) error {
	provider, err := createProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create cache provider: %w", err)
	}
	defaultProvider = provider
	return nil
}

// InitDefault 使用默认内存缓存初始化
func InitDefault() error {
	return Init(Config{
		Type:        "memory",
		NumCounters: 1000000,
		MaxCost:     268435456, // 256MB（原 1GB 占用过高）
		BufferItems: 64,
		Metrics:     true,
	})
}

// GetDefault 获取默认缓存提供者
func GetDefault() Provider {
	return defaultProvider
}

func createProvider(cfg Config) (Provider, error) {
	switch cfg.Type {
	case "memory":
		memConfig := MemoryConfig{
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
			memConfig.MaxCost = 268435456 // 256MB（原 1GB 占用过高）
		}
		if memConfig.BufferItems == 0 {
			memConfig.BufferItems = 64
		}
		return NewMemoryCache(memConfig)
	case "redis":
		return NewRedisCache(RedisConfig{
			Address:  cfg.Address,
			Password: cfg.Password,
			DB:       cfg.DB,
			PoolSize: cfg.PoolSize,
		})
	default:
		return nil, fmt.Errorf("unsupported cache type: %s", cfg.Type)
	}
}
