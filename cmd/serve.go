package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/core"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/internal/vipsfile"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	serveLog        = utils.ForModule("Serve")
	dependenciesLog = utils.ForModule("Dependencies")
	vipsLog         = utils.ForModule("VIPS")
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start API server",
	Run: func(cmd *cobra.Command, args []string) {
		initCommandLogger()
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
	db, err := database.New(cfg)
	if err != nil {
		return nil, err
	}

	// 自动迁移数据库
	if err := database.AutoMigrate(db); err != nil {
		_ = database.Close(db)
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}
	dependenciesLog.Infof("Database migration completed")

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
	dependenciesLog.Infof("Cache initialized from config")

	configManager := configSvc.NewManager(db, "./data")
	if err := configManager.Initialize(); err != nil {
		_ = database.Close(db)
		return nil, err
	}

	dependenciesLog.Infof("Config cache enabled")

	storageConfigs, err := configManager.GetStorageConfigs(context.Background())
	if err == nil && len(storageConfigs) > 0 {
		if err := storage.InitStorage(storageConfigs); err != nil {
			dependenciesLog.Warnf("Failed to init storage: %v", err)
		} else {
			dependenciesLog.Infof("Storage initialized from database configs")
		}
	} else {
		if err != nil {
			dependenciesLog.Warnf("Failed to get storage configs: %v", err)
		}
		if err := storage.InitStorage([]storage.StorageConfig{}); err != nil {
			dependenciesLog.Warnf("Failed to init default storage: %v", err)
		} else {
			dependenciesLog.Infof("Default storage initialized")
		}
	}

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

	utils.InitLogger(config.IsDevelopment())

	dataDir := utils.GetDataDir()

	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		exitWithErrorf("Failed to create data directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "temp"), os.ModePerm); err != nil {
		exitWithErrorf("Failed to create temp directory: %v", err)
	}

	if err := vipsfile.Startup(&vips.Config{
		MaxCacheMem:      1,
		MaxCacheSize:     1,
		MaxCacheFiles:    0,
		ConcurrencyLevel: 2,
	}); err != nil {
		exitWithErrorf("Failed to initialize govips: %v", err)
	}
	defer vipsfile.Shutdown()

	vipsLog.Infof("Govips initialized with cache limited to 1 byte / 1 entry")
	if config.IsDevelopment() {
		utils.LogMemoryStats("VIPS_INIT")
	}

	deps, err := InitDependencies(cfg)
	if err != nil {
		exitWithErrorf("Failed to initialize dependencies: %v", err)
	}
	defer func() { _ = deps.Close() }()

	if err := InitDatabase(deps); err != nil {
		exitWithErrorf("Failed to initialize database: %v", err)
	}

	worker.InitGlobalPool(cfg.WorkerCount, 1000)

	sweeperCtx, sweeperCancel := context.WithCancel(context.Background())
	defer sweeperCancel()
	worker.StartVariantSweeper(sweeperCtx, deps.DB, deps.Converter.TriggerConversionFromSweeper)

	// 初始化 JWT
	api.SetAuthKeysRepo(deps.Repositories.KeysRepo)
	if err := api.TokenInitFromConfig(cfg, deps.ConfigManager); err != nil {
		exitWithErrorf("Failed to initialize JWT: %v", err)
	}

	serverDeps := &core.ServerDependencies{
		DB:            deps.DB,
		Repositories:  deps.Repositories,
		ConfigManager: deps.ConfigManager,
		Converter:     deps.Converter,
		JWTService:    api.GetJWTService(),
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
		serveLog.Infof("Server started on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		serveLog.Errorf("Server unexpectedly stopped: %v", err)
	case sig := <-quit:
		serveLog.Infof("Received signal: %v, shutting down", sig)
	}
	serveLog.Infof("Shutting down server")

	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), cfg.ServerWriteTimeout)
	defer cancelHTTP()

	if err := server.Shutdown(httpCtx); err != nil {
		serveLog.Warnf("Server forced to shutdown: %v", err)
	}

	sweeperCancel()

	workerCtx, cancelWorkers := context.WithTimeout(context.Background(), cfg.ServerWriteTimeout)
	defer cancelWorkers()

	if err := worker.ShutdownGlobalPool(workerCtx); err != nil {
		serveLog.Warnf("Worker pool did not drain before shutdown deadline: %v", err)
		if rollbackErr := resetInFlightVariantWork(deps); rollbackErr != nil {
			serveLog.Warnf("Failed to reset in-flight variant work during shutdown: %v", rollbackErr)
		}
	}

	cleanup()
	serveLog.Infof("Server exited")
}

// InitDatabase 初始化数据库
func InitDatabase(deps *Dependencies) error {
	serveLog.Infof("Initializing database")

	password, err := deps.Repositories.AccountsRepo.CreateDefaultAdminUser()
	if err != nil {
		return err
	}
	if password != "" {
		serveLog.Warnf("========================================")
		serveLog.Warnf("默认管理员用户创建成功")
		serveLog.Warnf("用户名: admin")
		serveLog.Warnf("密码: %s", password)
		serveLog.Warnf("请登录后立即修改默认密码")
		serveLog.Warnf("========================================")
	} else {
		serveLog.Infof("Admin user already exists, skipping creation")
	}
	return nil
}

func resetInFlightVariantWork(deps *Dependencies) error {
	return resetVariantWorkSnapshots(deps, worker.CurrentInFlightTasks())
}

func resetVariantWorkSnapshots(deps *Dependencies, snapshots []worker.InFlightTaskSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	variantIDs := make([]uint, 0)
	imageIDs := make([]uint, 0)
	seenVariants := make(map[uint]struct{})
	seenImages := make(map[uint]struct{})

	for _, snapshot := range snapshots {
		if snapshot.ImageID > 0 {
			if _, ok := seenImages[snapshot.ImageID]; !ok {
				seenImages[snapshot.ImageID] = struct{}{}
				imageIDs = append(imageIDs, snapshot.ImageID)
			}
		}
		for _, variantID := range snapshot.VariantIDs {
			if variantID == 0 {
				continue
			}
			if _, ok := seenVariants[variantID]; ok {
				continue
			}
			seenVariants[variantID] = struct{}{}
			variantIDs = append(variantIDs, variantID)
		}
	}

	variantRepo := images.NewVariantRepository(deps.DB)
	resetVariants, err := variantRepo.ResetVariantsToPending(variantIDs)
	if err != nil {
		return err
	}

	resetImages, err := deps.Repositories.ImagesRepo.ResetProcessingVariantStatus(imageIDs, models.ImageVariantStatusNone)
	if err != nil {
		return err
	}

	serveLog.Infof("Reset %d in-flight variants and %d images during shutdown", resetVariants, resetImages)
	return nil
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
		return cache.Config{
			Type:        "memory",
			NumCounters: cfg.CacheNumCounters,
			MaxCost:     cfg.CacheMaxCost,
			BufferItems: 64,
			Metrics:     true,
		}
	default:
		// 未知类型时使用内存缓存
		dependenciesLog.Warnf("Unknown cache type '%s', using memory cache", cfg.CacheType)
		return cache.Config{
			Type:        "memory",
			NumCounters: cfg.CacheNumCounters,
			MaxCost:     cfg.CacheMaxCost,
			BufferItems: 64,
			Metrics:     true,
		}
	}
}
