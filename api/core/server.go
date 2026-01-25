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
	"github.com/gin-contrib/cors"

	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

// 启动gin
func setupRouter() (*gin.Engine, func()) {
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

	//router.SetTrustedProxies(nil)

	// P0 修复：限制上传文件大小
	// 限制内存中 multipart 表单的最大大小（50MB）
	router.MaxMultipartMemory = 50 << 20 // 50MB

	// 并发限制
	concurrencyLimiter := middleware.NewConcurrencyLimiter(300)
	router.Use(concurrencyLimiter.Middleware())

	// P1 修复：请求体大小限制（100MB）
	router.Use(middleware.RequestSizeLimit(100 << 20))

	// P1 修复：添加请求ID追踪中间件
	router.Use(middleware.RequestID())

	// P1 修复：添加基础监控指标
	router.Use(middleware.Metrics())

	// 速率限制
	authRateLimiter := middleware.NewIPRateLimiter(0.5, 5, 10*time.Minute)
	generalRateLimiter := middleware.NewIPRateLimiter(10, 20, 10*time.Minute)
	cleanup := func() {
		authRateLimiter.StopCleanup()
		generalRateLimiter.StopCleanup()
	}

	router.GET("/health", func(context *gin.Context) {
		// P1 修复：改进健康检查，添加数据库、缓存、存储检查
		health := gin.H{
			"status":  "ok",
			"uptime":  time.Since(startTime).Round(time.Second).String(),
			"version": config.Version,
			"checks": gin.H{
				"database": checkDatabaseHealth(),
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
	// P1 修复：添加监控指标端点
	router.GET("/metrics", func(context *gin.Context) {
		context.JSON(http.StatusOK, middleware.GetMetrics())
	})

	// 公共接口
	publicGroup := router.Group("/images")
	//publicGroup.Use(generalRateLimiter.Middleware())
	{
		publicGroup.GET("/:identifier", images2.GetImageHandler) //GET /images/{photo}
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
				imagesGroup.POST("/upload", images2.UploadImageHandler)   // POST /api/v1/images/upload (single file)
				imagesGroup.POST("/uploads", images2.UploadImagesHandler) // POST /api/v1/images/uploads (multiple files)

				imagesGroup.POST("", images2.ImageListHandler)                       // POST /api/v1/images/list
				imagesGroup.POST("/delete", images2.DeleteImagesHandler)             // POST /api/v1/images/delete
				imagesGroup.DELETE("/:identifier", images2.DeleteSingleImageHandler) // POST /api/v1/images/{photo}
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
// P1 修复：健康检查辅助函数
func checkDatabaseHealth() string {
	if db := dbcore.GetDBInstance(); db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return "error: " + err.Error()
		}
		if err := sqlDB.Ping(); err != nil {
			return "unavailable: " + err.Error()
		}
		return "ok"
	}
	return "not initialized"
}

func checkCacheHealth() string {
	if cache.GlobalManager != nil {
		return "ok"
	}
	return "not initialized"
}

func checkStorageHealth() string {
	storageClient, err := storage.GetStorage(config.Get().Server.StorageConfig.Type)
	if err != nil {
		return "error: " + err.Error()
	}
	if storageClient != nil {
		return "ok"
	}
	return "not configured"
}

func StartServer() (*http.Server, func()) {
	cfg := config.Get()
	router, clean := setupRouter()

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	return srv, clean
}
