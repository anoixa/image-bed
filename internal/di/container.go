package di

import (
	"fmt"
	"log"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/storage"
)

// Container 依赖注入容器 - 管理所有服务的生命周期
type Container struct {
	config         *config.Config
	storageFactory *storage.Factory
	cacheFactory   *cache.Factory
}

// NewContainer 创建新的依赖注入容器
func NewContainer(cfg *config.Config) *Container {
	return &Container{
		config: cfg,
	}
}

// Init 初始化所有服务
func (c *Container) Init() error {
	log.Println("Initializing DI container...")

	// 初始化存储工厂
	if err := c.initStorageFactory(); err != nil {
		return fmt.Errorf("failed to initialize storage factory: %w", err)
	}

	// 初始化缓存工厂
	if err := c.initCacheFactory(); err != nil {
		return fmt.Errorf("failed to initialize cache factory: %w", err)
	}

	log.Println("DI container initialized successfully")
	return nil
}

// initStorageFactory 初始化存储工厂
func (c *Container) initStorageFactory() error {
	factory, err := storage.NewFactory(c.config)
	if err != nil {
		return err
	}
	c.storageFactory = factory
	log.Println("Storage factory initialized")
	return nil
}

// initCacheFactory 初始化缓存工厂
func (c *Container) initCacheFactory() error {
	factory, err := cache.NewFactory(c.config)
	if err != nil {
		return err
	}
	c.cacheFactory = factory
	log.Println("Cache factory initialized")
	return nil
}

// GetStorageFactory 获取存储工厂
func (c *Container) GetStorageFactory() *storage.Factory {
	return c.storageFactory
}

// GetCacheFactory 获取缓存工厂
func (c *Container) GetCacheFactory() *cache.Factory {
	return c.cacheFactory
}

// GetConfig 获取配置
func (c *Container) GetConfig() *config.Config {
	return c.config
}

// Close 关闭所有服务
func (c *Container) Close() error {
	log.Println("Closing DI container...")

	if c.cacheFactory != nil {
		if err := c.cacheFactory.Close(); err != nil {
			log.Printf("Error closing cache factory: %v", err)
		}
	}

	log.Println("DI container closed")
	return nil
}
