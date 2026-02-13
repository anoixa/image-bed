package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/cache/redis"
	"github.com/anoixa/image-bed/cache/ristretto"
	"github.com/anoixa/image-bed/config"
)

// Factory 缓存工厂 - 负责创建和管理缓存提供者
type Factory struct {
	provider Provider
}

// Config 缓存配置
type Config struct {
	Provider  string
	Redis     RedisConfig
	Ristretto RistrettoConfig
}

// RedisConfig Redis配置
type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

// RistrettoConfig Ristretto配置
type RistrettoConfig struct {
	NumCounters int64
	MaxCost     int64
	BufferItems int64
	Metrics     bool
}

// NewFactory 创建新的缓存工厂
func NewFactory(cfg *config.Config) (*Factory, error) {
	provider, err := createProvider(cfg)
	if err != nil {
		return nil, err
	}

	return &Factory{
		provider: provider,
	}, nil
}

// createProvider 根据配置创建缓存提供者
func createProvider(cfg *config.Config) (Provider, error) {
	providerType := cfg.Server.CacheConfig.Provider
	if providerType == "" {
		providerType = "memory"
		log.Println("Cache provider not specified, using default: memory")
	}

	switch providerType {
	case "redis":
		return redis.NewRedis(cfg)
	case "memory", "ristretto":
		ristrettoConfig := ristretto.Config{
			NumCounters: cfg.Server.CacheConfig.Ristretto.NumCounters,
			MaxCost:     cfg.Server.CacheConfig.Ristretto.MaxCost,
			BufferItems: cfg.Server.CacheConfig.Ristretto.BufferItems,
			Metrics:     cfg.Server.CacheConfig.Ristretto.Metrics,
		}
		return ristretto.NewRistretto(ristrettoConfig)
	default:
		return nil, fmt.Errorf("unsupported cache provider: %s", providerType)
	}
}

// GetProvider 获取缓存提供者
func (f *Factory) GetProvider() Provider {
	return f.provider
}

// Close 关闭缓存
func (f *Factory) Close() error {
	if f.provider != nil {
		return f.provider.Close()
	}
	return nil
}

// --- 便捷方法 ---

// Set 设置缓存项
func (f *Factory) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if f.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return f.provider.Set(ctx, key, value, expiration)
}

// Get 获取缓存项
func (f *Factory) Get(ctx context.Context, key string, dest interface{}) error {
	if f.provider == nil {
		return ErrCacheMiss
	}
	err := f.provider.Get(ctx, key, dest)
	// 处理来自不同实现的 ErrCacheMiss
	if err != nil {
		if err.Error() == "cache miss" {
			return ErrCacheMiss
		}
	}
	return err
}

// Delete 删除缓存项
func (f *Factory) Delete(ctx context.Context, key string) error {
	if f.provider == nil {
		return nil
	}
	return f.provider.Delete(ctx, key)
}

// Exists 检查缓存项是否存在
func (f *Factory) Exists(ctx context.Context, key string) (bool, error) {
	if f.provider == nil {
		return false, nil
	}
	return f.provider.Exists(ctx, key)
}
