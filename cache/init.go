package cache

import (
	"log"

	"github.com/anoixa/image-bed/config"
)

var (
	// GlobalManager 全局缓存管理器
	GlobalManager *Manager
)

// InitCache 初始化缓存系统
func InitCache(cfg *config.Config) {
	cacheConfig := Config{
		Provider: cfg.Server.CacheConfig.Provider,
	}

	switch cfg.Server.CacheConfig.Provider {
	case "redis":
		cacheConfig.Redis = RedisConfig{
			Address:  cfg.Server.CacheConfig.Redis.Address,
			Password: cfg.Server.CacheConfig.Redis.Password,
			DB:       cfg.Server.CacheConfig.Redis.DB,
		}
	case "memory", "ristretto":
		cacheConfig.Ristretto = RistrettoConfig{
			NumCounters: cfg.Server.CacheConfig.Ristretto.NumCounters,
			MaxCost:     cfg.Server.CacheConfig.Ristretto.MaxCost,
			BufferItems: cfg.Server.CacheConfig.Ristretto.BufferItems,
			Metrics:     cfg.Server.CacheConfig.Ristretto.Metrics,
		}
	default:
		// 使用默认（ristretto）
		if cacheConfig.Provider == "" {
			cacheConfig.Provider = "memory"
			cacheConfig.Ristretto = RistrettoConfig{
				NumCounters: cfg.Server.CacheConfig.Ristretto.NumCounters,
				MaxCost:     cfg.Server.CacheConfig.Ristretto.MaxCost,
				BufferItems: cfg.Server.CacheConfig.Ristretto.BufferItems,
				Metrics:     cfg.Server.CacheConfig.Ristretto.Metrics,
			}
			log.Printf("Cache provider not specified, using default: ristretto")
		}
	}

	var err error
	GlobalManager, err = NewManager(cacheConfig)
	if err != nil {
		log.Fatalf("Failed to initialize cache manager: %v", err)
	}

	log.Printf("Cache manager initialized with provider: %s", cacheConfig.Provider)
}

// CloseCache 关闭缓存系统
func CloseCache() {
	if GlobalManager != nil {
		err := GlobalManager.Close()
		if err != nil {
			log.Printf("Error closing cache: %v", err)
		}
	}
}
