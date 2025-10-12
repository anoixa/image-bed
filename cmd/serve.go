package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	//_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/core"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/repo/accounts"
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

func RunServer() {
	// 加载配置
	config.InitConfig()
	cfg := config.Get()

	// 创建资源目录
	if err := os.MkdirAll("./data", os.ModePerm); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// 初始化db, jwt, storage, cache
	InitDatabase(cfg)
	storage.InitStorage(cfg)
	cache.InitCache(cfg)
	if err := api.TokenInit(cfg.Server.Jwt.Secret, cfg.Server.Jwt.ExpiresIn, cfg.Server.Jwt.RefreshExpiresIn); err != nil {
		log.Fatalf("Failed to initialize JWT %s", err)
	}

	// 启动gin
	server, cleanup := core.StartServer()
	go func() {
		log.Printf("Server started on %s", cfg.Server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// pprof
	//go func() {
	//	log.Println("Starting pprof server on :6060")
	//	if err := http.ListenAndServe("localhost:6060", nil); err != nil {
	//		log.Fatalf("pprof server failed: %v", err)
	//	}
	//}()

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

	// 关闭缓存
	cache.CloseCache()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	if err := dbcore.CloseDB(); err != nil {
		log.Printf("Error closing database: %v", err)
	}

	log.Println("Server exited successfully")
}

// InitDatabase init database
func InitDatabase(cfg *config.Config) {
	dbcore.InitDB(cfg)
	instance := dbcore.GetDBInstance()
	log.Printf("Initializing database, database type: %s", cfg.Server.DatabaseConfig.Type)

	// 自动DDL
	err := dbcore.AutoMigrateDB(instance)
	if err != nil {
		log.Fatalf("Failed to auto migrate database: %v", err)
	}

	// 创建默认管理员用户
	accounts.CreateDefaultAdminUser()

	log.Println("Database initialized successfully")
}
