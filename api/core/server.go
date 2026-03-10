package core

import (
	"log"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/auth"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Repositories 所有数据库仓库
type Repositories struct {
	AccountsRepo *accounts.Repository
	DevicesRepo  *accounts.DeviceRepository
	ImagesRepo   *images.Repository
	AlbumsRepo   *albums.Repository
	KeysRepo     *keys.Repository
}

// ServerVersion 服务器版本信息
type ServerVersion struct {
	Version    string
	CommitHash string
}

// ServerDependencies 服务器依赖项
type ServerDependencies struct {
	DB            *gorm.DB
	Repositories  *Repositories
	ConfigManager *configSvc.Manager
	Converter     *imageSvc.Converter
	JWTService    *auth.JWTService
	Config        *config.Config
	CacheProvider cache.Provider
	ServerVersion ServerVersion
}

// 启动gin
func setupRouter(deps *ServerDependencies) (*gin.Engine, func()) {
	cfg := deps.Config
	router := gin.New()

	// 仅在开发版本时启用 gin 日志
	if config.IsDevelopment() {
		gin.SetMode(gin.DebugMode)
		router.Use(gin.Logger())
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:              cfg.GetCorsOrigins(),
		AllowMethods:              []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:              []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		AllowCredentials:          true,
		MaxAge:                    12 * time.Hour,
		OptionsResponseStatusCode: 204,
	}))

	if err := router.SetTrustedProxies(nil); err != nil {
		log.Printf("Warning: Failed to set trusted proxies: %v", err)
	}

	router.MaxMultipartMemory = int64(cfg.UploadMaxSizeMB) << 20
	concurrencyLimiter := middleware.NewConcurrencyLimiter(100)
	router.Use(concurrencyLimiter.Middleware())
	requestBodyLimit := int64(cfg.UploadMaxBatchTotalMB) * 2 << 20
	if requestBodyLimit < 100<<20 {
		requestBodyLimit = 100 << 20 // 最小 100MB
	}
	router.Use(middleware.MaxBytesReader(requestBodyLimit))
	router.Use(middleware.RequestID())
	router.Use(middleware.Metrics())

	// 速率限制器
	authRateLimiter := middleware.NewIPRateLimiter(cfg.RateLimitAuthRPS, cfg.RateLimitAuthBurst, cfg.RateLimitExpireTime)
	apiRateLimiter := middleware.NewIPRateLimiter(cfg.RateLimitApiRPS, cfg.RateLimitApiBurst, cfg.RateLimitExpireTime)
	imageRateLimiter := middleware.NewIPRateLimiter(cfg.RateLimitImageRPS, cfg.RateLimitImageBurst, cfg.RateLimitExpireTime)
	cleanup := func() {
		authRateLimiter.StopCleanup()
		apiRateLimiter.StopCleanup()
		imageRateLimiter.StopCleanup()
	}

	var jwtService *auth.JWTService
	var loginService *auth.LoginService

	if deps.JWTService != nil {
		jwtService = deps.JWTService
	} else if deps.ConfigManager != nil {
		var err error
		jwtService, err = auth.NewJWTService(deps.ConfigManager, deps.Repositories.KeysRepo)
		if err != nil {
			log.Printf("[Server] Failed to initialize JWT service from config: %v, using defaults", err)
		}
	}

	if jwtService != nil {
		loginService = auth.NewLoginService(deps.Repositories.AccountsRepo, deps.Repositories.DevicesRepo, jwtService)
		api.SetJWTService(jwtService)
	}

	routerDeps := &RouterDependencies{
		DB:               deps.DB,
		Repositories:     deps.Repositories,
		ConfigManager:    deps.ConfigManager,
		Converter:        deps.Converter,
		JWTService:       jwtService,
		LoginService:     loginService,
		AuthRateLimiter:  authRateLimiter,
		APIRateLimiter:   apiRateLimiter,
		ImageRateLimiter: imageRateLimiter,
		CacheProvider:    deps.CacheProvider,
		ServerVersion:    deps.ServerVersion,
		Config:           deps.Config,
	}
	RegisterRoutes(router, routerDeps)

	return router, cleanup
}

// StartServer 创建 http.Server
func StartServer(deps *ServerDependencies) (*http.Server, func()) {
	cfg := deps.Config
	router, clean := setupRouter(deps)

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  cfg.ServerReadTimeout,
		WriteTimeout: cfg.ServerWriteTimeout,
		IdleTimeout:  cfg.ServerIdleTimeout,
	}

	return srv, clean
}
