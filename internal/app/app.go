package app

import (
	"fmt"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	cryptoservice "github.com/anoixa/image-bed/internal/services/crypto"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/internal/services/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
)

// Container 依赖注入容器 - 管理所有服务的生命周期
type Container struct {
	config          *config.Config
	databaseFactory *database.Factory
	configManager   *configSvc.Manager
	converter       *image.Converter

	AccountsRepo *accounts.Repository
	DevicesRepo  *accounts.DeviceRepository
	ImagesRepo   *images.Repository
	AlbumsRepo   *albums.Repository
	KeysRepo     *keys.Repository
}

// GetConverter 获取图片转换器
func (c *Container) GetConverter() *image.Converter {
	if c.converter == nil {
		db := c.databaseFactory.GetProvider().DB()
		variantRepo := images.NewVariantRepository(db)
		c.converter = image.NewConverter(c.configManager, variantRepo, storage.GetDefault())
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

	return nil
}

// initConfigManager 初始化配置管理器
func (c *Container) initConfigManager() error {
	// 初始化 ConfigManager
	manager := configSvc.NewManager(c.databaseFactory.GetProvider().DB(), "./data")
	if err := manager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}
	c.configManager = manager
	utils.LogIfDev("Config manager initialized")
	return nil
}

// SetupConfigCache 为 ConfigManager 设置缓存
func (c *Container) SetupConfigCache() {
	if c.configManager == nil {
		return
	}

	// 使用默认缓存提供者
	cacheProvider := cache.GetDefault()
	if cacheProvider != nil {
		c.configManager.SetCache(cacheProvider, 0) // 0 表示使用默认 TTL
		fmt.Println("[Container] Config cache enabled")
	}
}

// initRepositories 初始化所有仓库
func (c *Container) initRepositories() {
	db := c.databaseFactory.GetProvider().DB()
	c.AccountsRepo = accounts.NewRepository(db)
	c.DevicesRepo = accounts.NewDeviceRepository(db)
	c.ImagesRepo = images.NewRepository(db)
	c.AlbumsRepo = albums.NewRepository(db)
	c.KeysRepo = keys.NewRepository(db)
	utils.LogIfDev("Repositories initialized")
}

// DB 获取数据库连接（兼容旧接口）
func (c *Container) DB() *database.Factory {
	return c.databaseFactory
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


// GetDatabaseFactory 获取数据库工厂
func (c *Container) GetDatabaseFactory() *database.Factory {
	return c.databaseFactory
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

	// 关闭所有缓存提供者
	// TODO: 如果有需要，在这里关闭缓存

	if c.databaseFactory != nil {
		if err := c.databaseFactory.Close(); err != nil {
			utils.LogIfDevf("Error closing database factory: %v", err)
		}
	}

	utils.LogIfDev("DI container closed")
	return nil
}
