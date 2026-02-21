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
		database.Close(db)
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}
	log.Println("[Dependencies] Cache initialized from config")

	// 初始化配置管理器
	configManager := configSvc.NewManager(db, "./data")
	if err := configManager.Initialize(); err != nil {
		database.Close(db)
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
	converter := imageSvc.NewConverter(configManager, variantRepo, storage.GetDefault())

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

	if err := os.MkdirAll("./data", os.ModePerm); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	if err := os.MkdirAll("./data/temp", os.ModePerm); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}

	deps, err := InitDependencies(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize dependencies: %v", err)
	}
	defer deps.Close()

	// 初始化数据库
	InitDatabase(deps)

	// 初始化异步任务协程池
	worker.InitGlobalPool(cfg.WorkerCount, 1000)

	// 初始化变体仓库和服务
	variantRepo := images.NewVariantRepository(deps.DB)
	retryScanner := imageSvc.NewRetryScanner(
		variantRepo,
		deps.Converter,
		5*time.Minute,
	)
	retryScanner.Start()

	// 初始化缩略图服务
	thumbnailSvc := imageSvc.NewThumbnailService(variantRepo, deps.ConfigManager, storage.GetDefault(), deps.Converter)
	thumbnailScanner := imageSvc.NewThumbnailScanner(
		deps.DB,
		deps.ConfigManager,
		thumbnailSvc,
	)
	thumbnailScanner.Start()

	// 启动孤儿任务扫描器
	orphanScanner := imageSvc.NewOrphanScanner(
		variantRepo,
		deps.Converter,
		thumbnailSvc,
		10*time.Minute, // 10分钟视为孤儿任务
		5*time.Minute,  // 每5分钟扫描一次
	)
	orphanScanner.Start()

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
	}

	// 启动gin
	server, cleanup := core.StartServer(serverDeps)
	go func() {
		log.Printf("Server started on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// 停止所有后台任务
	retryScanner.Stop()
	thumbnailScanner.Stop()
	orphanScanner.Stop()

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
func InitDatabase(deps *Dependencies) {
	log.Println("Initializing database...")

	// 自动迁移数据库
	if err := database.AutoMigrate(deps.DB); err != nil {
		log.Fatalf("Failed to auto migrate database: %v", err)
	}
	log.Println("Database migration completed")

	// 创建默认账户
	deps.Repositories.AccountsRepo.CreateDefaultAdminUser()
}

// cleanupTempFiles 清理临时文件
func cleanupTempFiles() {
	tempDir := "./data/temp"
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("Failed to read temp directory: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			path := filepath.Join(tempDir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("Failed to remove temp file %s: %v", path, err)
			}
		}
	}
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
			MaxCost:     1073741824, // 1GB
			BufferItems: 64,
			Metrics:     true,
		}
	default:
		// 未知类型时使用内存缓存
		log.Printf("[Dependencies] Unknown cache type '%s', using memory cache", cfg.CacheType)
		return cache.Config{
			Type:        "memory",
			NumCounters: 1000000,
			MaxCost:     1073741824,
			BufferItems: 64,
			Metrics:     true,
		}
	}
}
