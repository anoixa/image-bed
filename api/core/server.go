package core

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/handler/admin"
	handlerAlbums "github.com/anoixa/image-bed/api/handler/albums"
	handlerImages "github.com/anoixa/image-bed/api/handler/images"
	"github.com/anoixa/image-bed/api/handler/key"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/services/auth"
	configSvc "github.com/anoixa/image-bed/config/db"
	imageSvc "github.com/anoixa/image-bed/internal/services/image"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var startTime = time.Now()

// simpleProvider 简单的数据库提供者实现
type simpleProvider struct {
	db *gorm.DB
}

func (p *simpleProvider) DB() *gorm.DB { return p.db }
func (p *simpleProvider) WithContext(ctx context.Context) *gorm.DB { return p.db.WithContext(ctx) }
func (p *simpleProvider) Transaction(fn database.TxFunc) error { return p.db.Transaction(fn) }
func (p *simpleProvider) TransactionWithContext(ctx context.Context, fn database.TxFunc) error {
	return p.db.WithContext(ctx).Transaction(fn)
}
func (p *simpleProvider) BeginTransaction() *gorm.DB { return p.db.Begin() }
func (p *simpleProvider) WithTransaction() *gorm.DB { return p.db.Begin() }
func (p *simpleProvider) AutoMigrate(models ...interface{}) error { return p.db.AutoMigrate(models...) }
func (p *simpleProvider) SQLDB() (*sql.DB, error) { return p.db.DB() }
func (p *simpleProvider) Ping() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
func (p *simpleProvider) Close() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
func (p *simpleProvider) Name() string { return "gorm" }

// Repositories 所有数据库仓库
type Repositories struct {
	AccountsRepo *accounts.Repository
	DevicesRepo  *accounts.DeviceRepository
	ImagesRepo   *images.Repository
	AlbumsRepo   *albums.Repository
	KeysRepo     *keys.Repository
}

// ServerDependencies 服务器依赖项
type ServerDependencies struct {
	DB            *gorm.DB
	Repositories  *Repositories
	ConfigManager *configSvc.Manager
	Converter     *imageSvc.Converter
}

// 启动gin
func setupRouter(deps *ServerDependencies) (*gin.Engine, func()) {
	cfg := config.Get()
	router := gin.New()

	// 全局中间件
	// 仅在开发版本时启用 gin 日志
	if config.CommitHash == "n/a" {
		gin.SetMode(gin.DebugMode)
		router.Use(gin.Logger())
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.BaseURL()},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.SetTrustedProxies(nil)

	// 限制上传文件大小
	router.MaxMultipartMemory = int64(cfg.UploadMaxSizeMB) << 20

	// 并发限制（100并发，避免内存过载）
	concurrencyLimiter := middleware.NewConcurrencyLimiter(100)
	router.Use(concurrencyLimiter.Middleware())

	// 请求体大小限制（批量上传总限制的2倍，确保能容纳批量请求）
	requestBodyLimit := int64(cfg.UploadMaxBatchTotalMB) * 2 << 20
	if requestBodyLimit < 100<<20 {
		requestBodyLimit = 100 << 20 // 最小 100MB
	}
	router.Use(middleware.MaxBytesReader(requestBodyLimit))

	// 请求ID追踪
	router.Use(middleware.RequestID())

	// 基础监控指标
	router.Use(middleware.Metrics())

	// 速率限制
	rl := cfg
	authRateLimiter := middleware.NewIPRateLimiter(rl.RateLimitAuthRPS, rl.RateLimitAuthBurst, rl.RateLimitExpireTime)
	apiRateLimiter := middleware.NewIPRateLimiter(rl.RateLimitApiRPS, rl.RateLimitApiBurst, rl.RateLimitExpireTime)
	imageRateLimiter := middleware.NewIPRateLimiter(rl.RateLimitImageRPS, rl.RateLimitImageBurst, rl.RateLimitExpireTime)
	cleanup := func() {
		authRateLimiter.StopCleanup()
		apiRateLimiter.StopCleanup()
		imageRateLimiter.StopCleanup()
	}

	router.GET("/health", func(context *gin.Context) {
		dbProvider := &simpleProvider{db: deps.DB}
		health := gin.H{
			"status":  "ok",
			"uptime":  time.Since(startTime).Round(time.Second).String(),
			"version": config.Version,
			"checks": gin.H{
				"database": checkDatabaseHealth(dbProvider),
				"cache":    checkCacheHealth(),
				"storage":  checkStorageHealth(),
			},
		}
		httpStatus := http.StatusOK
		for _, checkResult := range health["checks"].(gin.H) {
			if result, ok := checkResult.(string); ok && result != "ok" {
				httpStatus = http.StatusServiceUnavailable
				break
			}
		}
		context.JSON(httpStatus, health)
	})
	router.GET("/version", func(context *gin.Context) {
		common.RespondSuccess(context, gin.H{
			"version": config.Version,
			"commit":  config.CommitHash,
		})
	})
	router.GET("/metrics", func(context *gin.Context) {
		context.JSON(http.StatusOK, middleware.GetMetrics())
	})

	// 初始化认证服务
	var tokenManager *auth.TokenManager
	var jwtService *auth.JWTService
	var loginService *auth.LoginService

	if deps.ConfigManager != nil {
		var err error
		tokenManager, err = auth.NewTokenManager(deps.ConfigManager)
		if err != nil {
			// 如果配置管理器初始化失败，使用默认配置
			log.Printf("[Server] Failed to initialize token manager from config: %v, using defaults", err)
		}
	}

	if tokenManager != nil {
		jwtService = auth.NewJWTService(tokenManager, deps.Repositories.KeysRepo)
		loginService = auth.NewLoginService(deps.Repositories.AccountsRepo, deps.Repositories.DevicesRepo, jwtService)
		api.SetTokenManager(tokenManager)
		api.SetJWTService(jwtService)
	}

	// 创建处理器（依赖注入）
	cacheProvider := cache.GetDefault()
	dbProvider := &simpleProvider{db: deps.DB}
	imageHandler := handlerImages.NewHandler(cacheProvider, deps.Repositories.ImagesRepo, dbProvider, deps.Converter, deps.ConfigManager)
	albumHandler := handlerAlbums.NewHandler(deps.Repositories.AlbumsRepo, cacheProvider)
	albumImageHandler := handlerAlbums.NewAlbumImageHandler(deps.Repositories.AlbumsRepo, deps.Repositories.ImagesRepo, cacheProvider)
	keyHandler := key.NewHandler(deps.Repositories.KeysRepo)
	loginHandler := api.NewLoginHandlerWithService(loginService)

	// 公共接口 - 图片获取（可能涉及大文件）
	publicGroup := router.Group("/images")
	publicGroup.Use(imageRateLimiter.Middleware())
	{
		publicGroup.GET("/:identifier", imageHandler.GetImage) //GET /images/{photo}
	}

	// 缩略图公共访问路由 - 缩略图生成可能需要时间
	thumbnailGroup := router.Group("/thumbnails")
	thumbnailGroup.Use(imageRateLimiter.Middleware())
	{
		thumbnailGroup.GET("/:identifier", imageHandler.GetThumbnail) //GET /thumbnails/{photo}?width=300
	}

	apiGroup := router.Group("/api")
	apiGroup.Use(func(context *gin.Context) { // 所有API禁止缓存
		context.Header("Cache-Control", "no-store")
		context.Next()
	})
	{
		authGroup := apiGroup.Group("/auth")
		authGroup.Use(authRateLimiter.Middleware())
		{
			authGroup.POST("/login", loginHandler.LoginHandlerFunc)          //POST /api/auth/login
			authGroup.POST("/refresh", loginHandler.RefreshTokenHandlerFunc) //POST /api/auth/refresh
			authGroup.POST("/logout", loginHandler.LogoutHandlerFunc)        //POST /api/auth/logout
		}

		v1 := apiGroup.Group("/v1")
		v1.Use(apiRateLimiter.Middleware())
		v1.Use(middleware.CombinedAuth())
		{
			// image
			imagesGroup := v1.Group("/images")
			imagesGroup.Use(middleware.Authorize("jwt", "static_token"))
			{
				imagesGroup.POST("/upload", imageHandler.UploadImage)                            // POST /api/v1/images/upload (single file)
				imagesGroup.POST("/uploads", imageHandler.UploadImages)                          // POST /api/v1/images/uploads (multiple files)
				imagesGroup.POST("/upload/chunked/init", imageHandler.InitChunkedUpload)         // POST /api/v1/images/upload/chunked/init
				imagesGroup.POST("/upload/chunked", imageHandler.UploadChunk)                    // POST /api/v1/images/upload/chunked
				imagesGroup.GET("/upload/chunked/status", imageHandler.GetChunkedUploadStatus)   // GET /api/v1/images/upload/chunked/status
				imagesGroup.POST("/upload/chunked/complete", imageHandler.CompleteChunkedUpload) // POST /api/v1/images/upload/chunked/complete

				imagesGroup.POST("", imageHandler.ListImages)                                    // POST /api/v1/images/list
				imagesGroup.POST("/delete", imageHandler.DeleteImages)                           // POST /api/v1/images/delete
				imagesGroup.DELETE("/:identifier", imageHandler.DeleteSingleImage)               // DELETE /api/v1/images/{photo}
				imagesGroup.PATCH("/:identifier/visibility", imageHandler.UpdateImageVisibility) // PATCH /api/v1/images/{photo}/visibility
			}

			// static token
			apiTokenGroup := v1.Group("/token")
			apiTokenGroup.Use(middleware.Authorize("jwt"))
			{
				apiTokenGroup.POST("", keyHandler.CreateStaticToken) // POST /api/v1/token
				apiTokenGroup.GET("", keyHandler.GetToken)           // GET /api/v1/token

				apiTokenGroup.POST("/:id/disable", keyHandler.DisableToken) // POST /api/v1/token/{id}/disable
				apiTokenGroup.POST("/:id/enable", keyHandler.EnableToken)   // POST /api/v1/token/{id}/enable
				apiTokenGroup.DELETE("/:id", keyHandler.RevokeToken)        // DELETE /api/v1/token/{id}
			}

			// albums
			albumsGroup := v1.Group("/albums")
			albumsGroup.Use(middleware.Authorize("jwt"))
			{
				albumsGroup.GET("", albumHandler.ListAlbumsHandler)         // GET /api/v1/albums
				albumsGroup.POST("", albumHandler.CreateAlbumHandler)       // POST /api/v1/albums
				albumsGroup.GET("/:id", albumHandler.GetAlbumDetailHandler) // GET /api/v1/albums/{id}
				albumsGroup.PUT("/:id", albumHandler.UpdateAlbumHandler)    // PUT /api/v1/albums/{id}
				albumsGroup.DELETE("/:id", albumHandler.DeleteAlbumHandler) // DELETE /api/v1/albums/{id}

				// 相册图片管理
				albumsGroup.POST("/:id/images", albumImageHandler.AddImagesToAlbumHandler)                // POST /api/v1/albums/{id}/images
				albumsGroup.DELETE("/:id/images/:imageId", albumImageHandler.RemoveImageFromAlbumHandler) // DELETE /api/v1/albums/{id}/images/{imageId}
			}

			// admin - 配置管理
			if deps.ConfigManager != nil {
				configHandler := admin.NewConfigHandler(deps.ConfigManager)
				adminGroup := v1.Group("/admin")
				adminGroup.Use(middleware.Authorize("jwt"))
				adminGroup.Use(middleware.RequireRole("admin"))
				{
					// 配置管理
					configsGroup := adminGroup.Group("/configs")
					{
						configsGroup.GET("", configHandler.ListConfigs)                   // GET /api/v1/admin/configs
						configsGroup.POST("", configHandler.CreateConfig)                 // POST /api/v1/admin/configs
						configsGroup.GET("/:id", configHandler.GetConfig)                 // GET /api/v1/admin/configs/:id
						configsGroup.PUT("/:id", configHandler.UpdateConfig)              // PUT /api/v1/admin/configs/:id
						configsGroup.DELETE("/:id", configHandler.DeleteConfig)           // DELETE /api/v1/admin/configs/:id
						configsGroup.POST("/:id/test", configHandler.TestConfig)          // POST /api/v1/admin/configs/:id/test
						configsGroup.POST("/:id/default", configHandler.SetDefaultConfig) // POST /api/v1/admin/configs/:id/default
						configsGroup.POST("/:id/enable", configHandler.EnableConfig)      // POST /api/v1/admin/configs/:id/enable
						configsGroup.POST("/:id/disable", configHandler.DisableConfig)    // POST /api/v1/admin/configs/:id/disable
					}

					// 存储提供者管理
					adminGroup.GET("/storage/providers", configHandler.ListStorageProviders)  // GET /api/v1/admin/storage/providers
					adminGroup.POST("/storage/reload/:id", configHandler.ReloadStorageConfig) // POST /api/v1/admin/storage/reload/:id

					// 转换配置管理
					conversionHandler := admin.NewConversionHandler(deps.ConfigManager)
					adminGroup.GET("/conversion", conversionHandler.GetConfig)  // GET /api/v1/admin/conversion
					adminGroup.PUT("/conversion", conversionHandler.UpdateConfig) // PUT /api/v1/admin/conversion
				}
			}
		}
	}

	return router, cleanup
}

// StartServer 创建 http.Server
func StartServer(deps *ServerDependencies) (*http.Server, func()) {
	cfg := config.Get()
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
