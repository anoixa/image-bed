package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/core"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start API server",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// Dependencies 服务器依赖项
type Dependencies struct {
	DB            *gorm.DB
	Repositories  *core.Repositories
	ConfigManager *configSvc.Manager
	Converter     *imageSvc.Converter
}

// InitDependencies 初始化所有依赖
func InitDependencies(cfg *config.Config) (*Dependencies, error) {
	// 初始化数据库
	db, err := database.New(cfg)
	if err != nil {
		return nil, err
	}

	// 自动迁移数据库
	if err := database.AutoMigrate(db); err != nil {
		_ = database.Close(db)
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}
	log.Println("[Dependencies] Database migration completed")

	// 初始化仓库
	repos := &core.Repositories{
		AccountsRepo: accounts.NewRepository(db),
		DevicesRepo:  accounts.NewDeviceRepository(db),
		ImagesRepo:   images.NewRepository(db),
		AlbumsRepo:   albums.NewRepository(db),
		KeysRepo:     keys.NewRepository(db),
	}

	// 从配置文件初始化缓存
	cacheCfg := buildCacheConfig(cfg)
	if err := cache.Init(cacheCfg); err != nil {
		_ = database.Close(db)
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}
	log.Println("[Dependencies] Cache initialized from config")

	// 初始化配置管理器
	configManager := configSvc.NewManager(db, "./data")
	if err := configManager.Initialize(); err != nil {
		_ = database.Close(db)
		return nil, err
	}

	// 设置缓存到配置管理器
	cacheProvider := cache.GetDefault()
	if cacheProvider != nil {
		configManager.SetCache(cacheProvider, 0)
		log.Println("[Dependencies] Config cache enabled")
	}

	// 初始化存储层
	storageConfigs, err := configManager.GetStorageConfigs(context.Background())
	if err == nil && len(storageConfigs) > 0 {
		if err := storage.InitStorage(storageConfigs); err != nil {
			log.Printf("[Dependencies] Warning: Failed to init storage: %v", err)
		} else {
			log.Println("[Dependencies] Storage initialized from database configs")
		}
	} else if err != nil {
		log.Printf("[Dependencies] Warning: Failed to get storage configs: %v", err)
	}

	// 初始化变体仓库和转换器
	variantRepo := images.NewVariantRepository(db)
	imageRepo := images.NewRepository(db)
	cacheHelper := cache.NewHelper(cache.GetDefault())
	converter := imageSvc.NewConverter(configManager, variantRepo, imageRepo, storage.GetDefault(), cacheHelper)

	return &Dependencies{
		DB:            db,
		Repositories:  repos,
		ConfigManager: configManager,
		Converter:     converter,
	}, nil
}

// Close 关闭所有依赖
func (d *Dependencies) Close() error {
	if d.DB != nil {
		return database.Close(d.DB)
	}
	return nil
}

func RunServer() {
	config.InitConfig()
	cfg := config.Get()

	dataDir := utils.GetDataDir()

	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "temp"), os.ModePerm); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}

	vips.Startup(&vips.Config{
		MaxCacheMem:   64 << 20, // 64MB
		MaxCacheSize:  100,
		MaxCacheFiles: 0,
	})
	defer vips.Shutdown()

	log.Println("[VIPS] Govips initialized with minimal cache (1 byte)")
	if config.CommitHash == "n/a" {
		utils.LogMemoryStats("VIPS_INIT")
	}

	deps, err := InitDependencies(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize dependencies: %v", err)
	}
	defer func() { _ = deps.Close() }()

	// 初始化数据库
	InitDatabase(deps)

	// 初始化异步任务协程池
	worker.InitGlobalPool(cfg.WorkerCount, 1000)

	// 初始化变体仓库和服务
	variantRepo := images.NewVariantRepository(deps.DB)
	imageRepo := images.NewRepository(deps.DB)

	// 启动重试扫描器
	retryScanner := imageSvc.NewRetryScanner(variantRepo, imageRepo, deps.Converter, 5*time.Minute)
	retryScanner.Start()
	defer retryScanner.Stop()

	// 初始化缩略图服务
	thumbnailSvc := imageSvc.NewThumbnailService(variantRepo, imageRepo, deps.ConfigManager, storage.GetDefault(), deps.Converter)

	// 启动缩略图扫描器
	thumbnailScanner := imageSvc.NewThumbnailScanner(deps.DB, deps.ConfigManager, thumbnailSvc)
	if err := thumbnailScanner.Start(); err != nil {
		log.Fatalf("Failed to start thumbnail scanner: %v", err)
	}
	defer thumbnailScanner.Stop()

	// 启动孤儿任务扫描器
	orphanScanner := imageSvc.NewOrphanScanner(
		variantRepo, deps.Converter, thumbnailSvc,
		10*time.Minute, // 10分钟视为孤儿任务
		5*time.Minute,  // 每5分钟扫描一次
	)
	orphanScanner.Start()
	defer orphanScanner.Stop()

	// 初始化 JWT
	if err := api.TokenInitFromManager(deps.ConfigManager); err != nil {
		log.Fatalf("Failed to initialize JWT: %s", err)
	}

	// 创建服务器依赖
	serverDeps := &core.ServerDependencies{
		DB:            deps.DB,
		Repositories:  deps.Repositories,
		ConfigManager: deps.ConfigManager,
		Converter:     deps.Converter,
		TokenManager:  api.GetTokenManager(),
		Config:        cfg,
		CacheProvider: cache.GetDefault(),
		ServerVersion: core.ServerVersion{
			Version:    config.Version,
			CommitHash: config.CommitHash,
		},
	}

	// 启动gin
	server, cleanup := core.StartServer(serverDeps)
	serverErrCh := make(chan error, 1)

	go func() {
		log.Printf("Server started on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		log.Printf("Server unexpectedly stopped: %v", err)
	case sig := <-quit:
		log.Printf("Received signal: %v, shutting down...", sig)
	}
	log.Println("Shutting down server...")

	// 停止全局 Worker 池
	worker.StopGlobalPool()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ServerWriteTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	cleanup()
	log.Println("Server exited")
}

// InitDatabase 初始化数据库
// 注意：数据库迁移已在 InitDependencies 中完成，这里只创建默认账户
func InitDatabase(deps *Dependencies) {
	log.Println("Initializing database...")

	// 创建默认账户
	deps.Repositories.AccountsRepo.CreateDefaultAdminUser()
}

// buildCacheConfig 从应用配置构建缓存配置
func buildCacheConfig(cfg *config.Config) cache.Config {
	switch cfg.CacheType {
	case "redis":
		return cache.Config{
			Type:     "redis",
			Address:  cfg.CacheRedisAddr,
			Password: cfg.CacheRedisPassword,
			DB:       cfg.CacheRedisDB,
		}
	case "memory", "":
		// 默认使用内存缓存
		return cache.Config{
			Type:        "memory",
			NumCounters: 1000000,
			MaxCost:     268435456,
			BufferItems: 64,
			Metrics:     true,
		}
	default:
		// 未知类型时使用内存缓存
		log.Printf("[Dependencies] Unknown cache type '%s', using memory cache", cfg.CacheType)
		return cache.Config{
			Type:        "memory",
			NumCounters: 1000000,
			MaxCost:     268435456,
			BufferItems: 64,
			Metrics:     true,
		}
	}
}
