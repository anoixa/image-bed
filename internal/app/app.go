package app

import (
	"fmt"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/images"
	cryptoservice "github.com/anoixa/image-bed/internal/services/crypto"
	configSvc "github.com/anoixa/image-bed/internal/services/config"
	"github.com/anoixa/image-bed/internal/repositories"
	"github.com/anoixa/image-bed/internal/services/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
)

// Container 依赖注入容器 - 管理所有服务的生命周期
type Container struct {
	config          *config.Config
	storageFactory  *storage.Factory
	cacheFactory    *cache.Factory
	databaseFactory *database.Factory
	configManager   *configSvc.Manager
	repositories    *repositories.Repositories
	converter       *image.Converter
}

// GetConverter 获取图片转换器
func (c *Container) GetConverter() *image.Converter {
	if c.converter == nil {
		db := c.databaseFactory.GetProvider().DB()
		variantRepo := images.NewVariantRepository(db)
		c.converter = image.NewConverter(c.configManager, variantRepo, c.storageFactory.GetDefault())
	}
	return c.converter
}

// NewContainer 创建新的依赖注入容器
func NewContainer(cfg *config.Config) *Container {
	return &Container{
		config: cfg,
	}
}

func (c *Container) Init() error {
	if err := c.InitDatabase(); err != nil {
		return err
	}
	if err := c.InitServices(); err != nil {
		return err
	}
	return nil
}

func (c *Container) InitDatabase() error {
	utils.LogIfDev("Initializing DI container...")

	if err := c.initDatabaseFactory(); err != nil {
		return fmt.Errorf("failed to initialize database factory: %w", err)
	}

	c.initRepositories()

	utils.LogIfDev("DI container initialized successfully")
	return nil
}

func (c *Container) InitServices() error {
	if err := c.initConfigManager(); err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	if err := c.initStorageFactory(); err != nil {
		return fmt.Errorf("failed to initialize storage factory: %w", err)
	}

	if err := c.initCacheFactory(); err != nil {
		return fmt.Errorf("failed to initialize cache factory: %w", err)
	}

	return nil
}

// initConfigManager 初始化配置管理器
func (c *Container) initConfigManager() error {
	manager := configSvc.NewManager(c.databaseFactory.GetProvider().DB(), "./data")
	if err := manager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}
	c.configManager = manager
	utils.LogIfDev("Config manager initialized")
	return nil
}

// initRepositories 初始化所有仓库
func (c *Container) initRepositories() {
	c.repositories = repositories.NewRepositories(c.databaseFactory.GetProvider())
	utils.LogIfDev("Repositories initialized")
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
	utils.LogIfDev("Database factory initialized")
	return nil
}

// initStorageFactory 初始化存储工厂
func (c *Container) initStorageFactory() error {
	db := c.databaseFactory.GetProvider().DB()
	factory, err := storage.NewFactory(db, c.GetCryptoService())
	if err != nil {
		return err
	}
	c.storageFactory = factory
	utils.LogIfDev("Storage factory initialized")
	return nil
}

// initCacheFactory 初始化缓存工厂
func (c *Container) initCacheFactory() error {
	db := c.databaseFactory.GetProvider().DB()
	factory, err := cache.NewFactory(db, c.GetCryptoService())
	if err != nil {
		return err
	}
	c.cacheFactory = factory
	utils.LogIfDev("Cache factory initialized")
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

// GetConfigManager 获取配置管理器
func (c *Container) GetConfigManager() *configSvc.Manager {
	return c.configManager
}

// GetCryptoService 获取加密服务
func (c *Container) GetCryptoService() *cryptoservice.Service {
	if c.configManager != nil {
		return c.configManager.GetCrypto()
	}
	return nil
}

// GetConfig 获取配置
func (c *Container) GetConfig() *config.Config {
	return c.config
}

// Close 关闭所有服务
func (c *Container) Close() error {
	utils.LogIfDev("Closing DI container...")

	if c.cacheFactory != nil {
		if err := c.cacheFactory.Close(); err != nil {
			utils.LogIfDevf("Error closing cache factory: %v", err)
		}
	}

	if c.databaseFactory != nil {
		if err := c.databaseFactory.Close(); err != nil {
			utils.LogIfDevf("Error closing database factory: %v", err)
		}
	}

	utils.LogIfDev("DI container closed")
	return nil
}
