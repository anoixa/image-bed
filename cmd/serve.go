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
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/internal/di"
	"github.com/anoixa/image-bed/utils/async"
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

func RunServer() {
	// 加载配置
	config.InitConfig()
	cfg := config.Get()

	// 创建资源目录
	if err := os.MkdirAll("./data", os.ModePerm); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	if err := os.MkdirAll("./data/temp", os.ModePerm); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}

	// 简陋的DI
	container := di.NewContainer(cfg)
	if err := container.Init(); err != nil {
		log.Fatalf("Failed to initialize DI container: %v", err)
	}

	// 初始化数据库
	InitDatabase(container)

	// 初始化异步任务协程池
	async.InitGlobalPool(cfg.Server.WorkerCount, 1000)

	// 初始化 JWT
	if err := api.TokenInit(cfg.Server.Jwt.Secret, cfg.Server.Jwt.ExpiresIn, cfg.Server.Jwt.RefreshExpiresIn); err != nil {
		log.Fatalf("Failed to initialize JWT %s", err)
	}

	// 创建服务器依赖
	deps := &core.ServerDependencies{
		StorageFactory: container.GetStorageFactory(),
		CacheFactory:   container.GetCacheFactory(),
		Repositories:   container.GetRepositories(),
	}

	// 启动gin
	server, cleanup := core.StartServer(deps)
	go func() {
		log.Printf("Server started on %s", cfg.Server.Addr())
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

	// 关闭异步任务池
	async.StopGlobalPool()

	// 关闭 DI 容器
	if err := container.Close(); err != nil {
		log.Printf("Error closing container: %v", err)
	}

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited successfully")
}

// InitDatabase init database using DI container
func InitDatabase(container *di.Container) {
	factory := container.GetDatabaseFactory()
	log.Printf("Initializing database, database type: %s", factory.GetProvider().Name())

	// 自动DDL
	if err := factory.AutoMigrate(); err != nil {
		log.Fatalf("Failed to auto migrate database: %v", err)
	}

	// 创建默认管理员用户
	repos := container.GetRepositories()
	if repos != nil && repos.Accounts != nil {
		repos.Accounts.CreateDefaultAdminUser()
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
	var cleaned int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			sessionDir := filepath.Join(tempDir, entry.Name())
			if err := os.RemoveAll(sessionDir); err == nil {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		log.Printf("Cleaned %d expired temp directories on startup", cleaned)
	}
}

// startChunkedUploadCleanup 启动分片上传会话定期清理
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
