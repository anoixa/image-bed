package core

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/utils"

	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	dashboardRepo "github.com/anoixa/image-bed/database/repo/dashboard"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/auth"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var serverLog = utils.ForModule("Server")

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
	SqlDB         *sql.DB
	Repositories  *Repositories
	VariantRepo   *images.VariantRepository
	DashboardRepo *dashboardRepo.Repository
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
		serverLog.Warnf("Failed to set trusted proxies: %v", err)
	}

	const (
		defaultMaxUploadSizeMB = 50
		multipartMemoryMB      = 8 // gin in-memory multipart buffer; actual size limit enforced per-request
		defaultMaxBatchTotalMB = 500
		minBatchRequestLimitMB = 100
		batchRequestLimitRatio = 2
		apiMaxConcurrency      = 100
		publicMaxConcurrency   = 200
	)

	router.MaxMultipartMemory = multipartMemoryMB << 20
	requestBodyLimit := int64(defaultMaxBatchTotalMB) * batchRequestLimitRatio << 20
	if requestBodyLimit < minBatchRequestLimitMB<<20 {
		requestBodyLimit = minBatchRequestLimitMB << 20
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

	jwtService := deps.JWTService
	var loginService *auth.LoginService
	if jwtService != nil {
		loginService = auth.NewLoginService(deps.Repositories.AccountsRepo, deps.Repositories.DevicesRepo, jwtService)
	}

	routerDeps := &RouterDependencies{
		VariantRepo:       deps.VariantRepo,
		DashboardRepo:     deps.DashboardRepo,
		SqlDB:             deps.SqlDB,
		Repositories:      deps.Repositories,
		ConfigManager:     deps.ConfigManager,
		Converter:         deps.Converter,
		JWTService:        jwtService,
		LoginService:      loginService,
		AuthRateLimiter:   authRateLimiter,
		APIRateLimiter:    apiRateLimiter,
		ImageRateLimiter:  imageRateLimiter,
		APIConcurrency:    middleware.NewConcurrencyLimiter(apiMaxConcurrency),
		PublicConcurrency: middleware.NewConcurrencyLimiter(publicMaxConcurrency),
		CacheProvider:     deps.CacheProvider,
		ServerVersion:     deps.ServerVersion,
		Config:            deps.Config,
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
