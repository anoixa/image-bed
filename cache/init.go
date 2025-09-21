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
	case "memory":
		cacheConfig.GoCache = GoCacheConfig{
			DefaultExpiration: cfg.Server.CacheConfig.Memory.DefaultExpiration,
			CleanupInterval:   cfg.Server.CacheConfig.Memory.CleanupInterval,
		}
	default:
		// 使用默认（go-cache）
		if cacheConfig.Provider == "" {
			cacheConfig.Provider = "memory"
			cacheConfig.GoCache = GoCacheConfig{
				DefaultExpiration: cfg.Server.CacheConfig.Memory.DefaultExpiration,
				CleanupInterval:   cfg.Server.CacheConfig.Memory.CleanupInterval,
			}
			log.Printf("Cache provider not specified, using default: gocache")
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
