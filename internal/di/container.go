package di

import (
	"fmt"
	"log"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/internal/repositories"
	"github.com/anoixa/image-bed/storage"
)

// Container 依赖注入容器 - 管理所有服务的生命周期
type Container struct {
	config          *config.Config
	storageFactory  *storage.Factory
	cacheFactory    *cache.Factory
	databaseFactory *database.Factory
	repositories    *repositories.Repositories
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

	// 初始化数据库工厂
	if err := c.initDatabaseFactory(); err != nil {
		return fmt.Errorf("failed to initialize database factory: %w", err)
	}

	// 初始化存储工厂
	if err := c.initStorageFactory(); err != nil {
		return fmt.Errorf("failed to initialize storage factory: %w", err)
	}

	// 初始化缓存工厂
	if err := c.initCacheFactory(); err != nil {
		return fmt.Errorf("failed to initialize cache factory: %w", err)
	}

	// 初始化 Repositories
	c.initRepositories()

	log.Println("DI container initialized successfully")
	return nil
}

// initRepositories 初始化所有仓库
func (c *Container) initRepositories() {
	c.repositories = repositories.NewRepositories(c.databaseFactory.GetProvider())
	log.Println("Repositories initialized")
}

// GetRepositories 获取所有仓库
func (c *Container) GetRepositories() *repositories.Repositories {
	return c.repositories
}

// GetDatabaseProvider 获取数据库提供者
func (c *Container) GetDatabaseProvider() database.Provider {
	if c.databaseFactory == nil {
		return nil
	}
	return c.databaseFactory.GetProvider()
}

// initDatabaseFactory 初始化数据库工厂
func (c *Container) initDatabaseFactory() error {
	factory, err := database.NewFactory(c.config)
	if err != nil {
		return err
	}
	c.databaseFactory = factory
	log.Println("Database factory initialized")
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

// GetDatabaseFactory 获取数据库工厂
func (c *Container) GetDatabaseFactory() *database.Factory {
	return c.databaseFactory
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

	if c.databaseFactory != nil {
		if err := c.databaseFactory.Close(); err != nil {
			log.Printf("Error closing database factory: %v", err)
		}
	}

	log.Println("DI container closed")
	return nil
}
