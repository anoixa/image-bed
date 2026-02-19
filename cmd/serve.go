package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/core"
	handlerImages "github.com/anoixa/image-bed/api/handler/images"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	configSvc "github.com/anoixa/image-bed/config/db"
	imageSvc "github.com/anoixa/image-bed/internal/services/image"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/spf13/cobra"
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
	DBFactory      *database.Factory
	DB             *database.GormProvider
	Repositories   *core.Repositories
	ConfigManager  *configSvc.Manager
	Converter      *imageSvc.Converter
}

// InitDependencies 初始化所有依赖
func InitDependencies(cfg *config.Config) (*Dependencies, error) {
	// 初始化数据库工厂
	dbFactory, err := database.NewFactory(cfg)
	if err != nil {
		return nil, err
	}

	// 获取数据库提供者
	gormProvider := dbFactory.GetProvider().(*database.GormProvider)
	db := gormProvider.DB()

	// 初始化仓库
	repos := &core.Repositories{
		AccountsRepo: accounts.NewRepository(db),
		DevicesRepo:  accounts.NewDeviceRepository(db),
		ImagesRepo:   images.NewRepository(db),
		AlbumsRepo:   albums.NewRepository(db),
		KeysRepo:     keys.NewRepository(db),
	}

	// 初始化配置管理器
	configManager := configSvc.NewManager(db, "./data")
	if err := configManager.Initialize(); err != nil {
		dbFactory.Close()
		return nil, err
	}

	// 设置缓存
	cacheProvider := cache.GetDefault()
	if cacheProvider != nil {
		configManager.SetCache(cacheProvider, 0)
		log.Println("[Dependencies] Config cache enabled")
	}

	// 初始化变体仓库和转换器
	variantRepo := images.NewVariantRepository(db)
	converter := imageSvc.NewConverter(configManager, variantRepo, storage.GetDefault())

	return &Dependencies{
		DBFactory:     dbFactory,
		DB:            gormProvider,
		Repositories:  repos,
		ConfigManager: configManager,
		Converter:     converter,
	}, nil
}

// Close 关闭所有依赖
func (d *Dependencies) Close() error {
	if d.DBFactory != nil {
		return d.DBFactory.Close()
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

	// 显式初始化依赖
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
	variantRepo := images.NewVariantRepository(deps.DB.DB())

	// 初始化重试扫描器
	retryScanner := imageSvc.NewRetryScanner(
		variantRepo,
		deps.Converter,
		5*time.Minute,
	)
	retryScanner.Start()

	// 初始化缩略图服务
	thumbnailSvc := imageSvc.NewThumbnailService(variantRepo, deps.ConfigManager, storage.GetDefault(), deps.Converter)
	thumbnailScanner := imageSvc.NewThumbnailScanner(
		deps.DB.DB(),
		deps.ConfigManager,
		thumbnailSvc,
	)
	thumbnailScanner.Start()

	// 启动孤儿任务扫描器（处理卡在 processing 状态的任务）
	orphanScanner := imageSvc.NewOrphanScanner(
		variantRepo,
		deps.Converter,
		thumbnailSvc,
		10*time.Minute, // 10分钟视为孤儿任务
		5*time.Minute,  // 每5分钟扫描一次
	)
	orphanScanner.Start()

	// 初始化 JWT（从数据库加载配置）
	if err := api.TokenInitFromManager(deps.ConfigManager); err != nil {
		log.Fatalf("Failed to initialize JWT: %s", err)
	}

	// 创建服务器依赖
	serverDeps := &core.ServerDependencies{
		DB:            deps.DB.DB(),
		Repositories:  deps.Repositories,
		ConfigManager: deps.ConfigManager,
		Converter:     deps.Converter,
	}

	// 启动gin
	server, cleanup := core.StartServer(serverDeps)
	go func() {
		log.Printf("Server started on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// 启动分片上传会话清理任务
	go startChunkedUploadCleanup()

	// 处理退出signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if cleanup != nil {
		cleanup()
		log.Println("Cleanup tasks finished.")
	}

	// 停止重试扫描器
	retryScanner.Stop()

	// 停止缩略图扫描器
	thumbnailScanner.Stop()

	// 停止孤儿任务扫描器
	orphanScanner.Stop()

	// 关闭异步任务池
	worker.StopGlobalPool()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited successfully")
}

// InitDatabase init database
func InitDatabase(deps *Dependencies) {
	log.Printf("Initializing database, database type: %s", deps.DB.Name())

	// 自动DDL
	if err := deps.DBFactory.AutoMigrate(); err != nil {
		log.Fatalf("Failed to auto migrate database: %v", err)
	}

	// 创建默认管理员用户
	if deps.Repositories.AccountsRepo != nil {
		deps.Repositories.AccountsRepo.CreateDefaultAdminUser()
	}

	log.Println("Database initialized successfully")

	// 启动时清理残留临时文件
	go cleanOldTempFiles()
}

// cleanOldTempFiles 清理超过24小时的临时文件
func cleanOldTempFiles() {
	tempDir := "./data/temp"
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		return
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("Failed to read temp directory: %v", err)
		return
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(tempDir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("Failed to remove old temp file %s: %v", path, err)
			}
		}
	}
}

// startChunkedUploadCleanup 定期清理过期的分片上传会话
func startChunkedUploadCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			handlerImages.CleanupExpiredSessions()
		}
	}
}
