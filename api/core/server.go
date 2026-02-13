package core

import (
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/handler/albums"
	images2 "github.com/anoixa/image-bed/api/handler/images"
	key2 "github.com/anoixa/image-bed/api/handler/key"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

// ServerDependencies 服务器依赖项
type ServerDependencies struct {
	StorageFactory *storage.Factory
	CacheFactory   *cache.Factory
}

// 启动gin
func setupRouter(deps *ServerDependencies) (*gin.Engine, func()) {
	cfg := config.Get()
	if config.CommitHash != "n/a" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// 全局中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.Server.BaseURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.SetTrustedProxies(nil)

	// 限制上传文件大小（50MB）
	router.MaxMultipartMemory = 50 << 20

	// 并发限制
	concurrencyLimiter := middleware.NewConcurrencyLimiter(300)
	router.Use(concurrencyLimiter.Middleware())

	// 请求体大小限制（100MB）
	router.Use(middleware.RequestSizeLimit(100 << 20))

	// 请求ID追踪
	router.Use(middleware.RequestID())

	// 基础监控指标
	router.Use(middleware.Metrics())

	// 速率限制
	authRateLimiter := middleware.NewIPRateLimiter(0.5, 5, 10*time.Minute)
	generalRateLimiter := middleware.NewIPRateLimiter(10, 20, 10*time.Minute)
	cleanup := func() {
		authRateLimiter.StopCleanup()
		generalRateLimiter.StopCleanup()
	}

	router.GET("/health", func(context *gin.Context) {
		health := gin.H{
			"status":  "ok",
			"uptime":  time.Since(startTime).Round(time.Second).String(),
			"version": config.Version,
			"checks": gin.H{
				"database": checkDatabaseHealth(),
				"cache":    checkCacheHealth(deps.CacheFactory),
				"storage":  checkStorageHealth(deps.StorageFactory),
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

	// 创建图片处理器（依赖注入）
	imageHandler := images2.NewHandler(deps.StorageFactory, deps.CacheFactory)

	// 公共接口
	publicGroup := router.Group("/images")
	//publicGroup.Use(generalRateLimiter.Middleware())
	{
		publicGroup.GET("/:identifier", imageHandler.GetImage) //GET /images/{photo}
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
			authGroup.POST("/login", api.LoginHandler)          //POST /api/auth/login
			authGroup.POST("/refresh", api.RefreshTokenHandler) //POST /api/auth/refresh
			authGroup.POST("/logout", api.LogoutHandler)        //POST /api/auth/logout
		}

		v1 := apiGroup.Group("/v1")
		v1.Use(generalRateLimiter.Middleware())
		v1.Use(middleware.CombinedAuth())
		{
			// image
			imagesGroup := v1.Group("/images")
			imagesGroup.Use(middleware.Authorize("jwt", "static_token"))
			{
				imagesGroup.POST("/upload", imageHandler.UploadImage)           // POST /api/v1/images/upload (single file)
				imagesGroup.POST("/uploads", imageHandler.UploadImages)         // POST /api/v1/images/uploads (multiple files)
				imagesGroup.POST("/upload/chunked/init", imageHandler.InitChunkedUpload)   // POST /api/v1/images/upload/chunked/init
				imagesGroup.POST("/upload/chunked", imageHandler.UploadChunk)              // POST /api/v1/images/upload/chunked
				imagesGroup.GET("/upload/chunked/status", imageHandler.GetChunkedUploadStatus) // GET /api/v1/images/upload/chunked/status
				imagesGroup.POST("/upload/chunked/complete", imageHandler.CompleteChunkedUpload) // POST /api/v1/images/upload/chunked/complete

				imagesGroup.POST("", imageHandler.ListImages)                       // POST /api/v1/images/list
				imagesGroup.POST("/delete", imageHandler.DeleteImages)             // POST /api/v1/images/delete
				imagesGroup.DELETE("/:identifier", imageHandler.DeleteSingleImage) // DELETE /api/v1/images/{photo}
			}

			// static token
			apiTokenGroup := v1.Group("/token")
			apiTokenGroup.Use(middleware.Authorize("jwt"))
			{
				apiTokenGroup.POST("", key2.CreateStaticToken) // POST /api/v1/token
				apiTokenGroup.GET("", key2.GetToken)           // GET /api/v1/token

				apiTokenGroup.POST("/:id/disable", key2.DisableToken) // POST /api/v1/token/{id}/disable
				apiTokenGroup.POST("/:id/enable", key2.EnableToken)   // POST /api/v1/token/{id}/enable
				apiTokenGroup.DELETE("/:id", key2.RevokeToken)        // DELETE /api/v1/token/{id}
			}

			// albums
			albumsGroup := v1.Group("/albums")
			albumsGroup.Use(middleware.Authorize("jwt"))
			{
				albumsGroup.POST("", albums.CreateAlbumHandler)       // POST /api/v1/albums
				albumsGroup.DELETE("/:id", albums.DeleteAlbumHandler) // DELETE /api/v1/albums/{id}
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
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	return srv, clean
}
