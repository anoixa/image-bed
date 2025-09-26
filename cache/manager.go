package cache

import (
	"fmt"
	"time"

	"github.com/anoixa/image-bed/cache/redis"
	"github.com/anoixa/image-bed/cache/ristretto"
	"github.com/anoixa/image-bed/cache/types"
)

// Manager 缓存管理器
type Manager struct {
	provider types.Cache
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

// NewManager 创建一个新的缓存管理器
func NewManager(config Config) (*Manager, error) {
	var provider types.Cache
	var err error

	switch config.Provider {
	case "redis":
		provider, err = redis.NewRedis(config.Redis.Address, config.Redis.Password, config.Redis.DB)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize redis provider: %w", err)
		}
	case "memory", "ristretto":
		ristrettoConfig := ristretto.Config{
			NumCounters: config.Ristretto.NumCounters,
			MaxCost:     config.Ristretto.MaxCost,
			BufferItems: config.Ristretto.BufferItems,
			Metrics:     config.Ristretto.Metrics,
		}
		var err error
		provider, err = ristretto.NewRistretto(ristrettoConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize ristretto provider: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported cache provider: %s", config.Provider)
	}

	return &Manager{
		provider: provider,
	}, nil
}

// Set 设置缓存项
func (m *Manager) Set(key string, value interface{}, expiration time.Duration) error {
	return m.provider.Set(key, value, expiration)
}

// Get 获取缓存项
func (m *Manager) Get(key string, dest interface{}) error {
	return m.provider.Get(key, dest)
}

// Delete 删除缓存项
func (m *Manager) Delete(key string) error {
	return m.provider.Delete(key)
}

// Exists 检查缓存项是否存在
func (m *Manager) Exists(key string) (bool, error) {
	return m.provider.Exists(key)
}

// Close 关闭缓存连接
func (m *Manager) Close() error {
	return m.provider.Close()
}
