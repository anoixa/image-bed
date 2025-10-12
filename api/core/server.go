package core

import (
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	images2 "github.com/anoixa/image-bed/api/handler/images"
	key2 "github.com/anoixa/image-bed/api/handler/key"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/gin-contrib/cors"

	"github.com/gin-gonic/gin"
)

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

	// 并发限制
	concurrencyLimiter := middleware.NewConcurrencyLimiter(300)
	router.Use(concurrencyLimiter.Middleware())

	// 速率限制
	authRateLimiter := middleware.NewIPRateLimiter(0.5, 5, 10*time.Minute)
	generalRateLimiter := middleware.NewIPRateLimiter(10, 20, 10*time.Minute)
	cleanup := func() {
		authRateLimiter.StopCleanup()
		generalRateLimiter.StopCleanup()
	}

	router.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "^_^") })
	router.GET("/version", func(c *gin.Context) {
		common.RespondSuccess(c, gin.H{
			"version": config.Version,
			"commit":  config.CommitHash,
		})
	})

	// 公共接口
	publicGroup := router.Group("/images")
	//publicGroup.Use(generalRateLimiter.Middleware())
	{
		publicGroup.GET("/:identifier", images2.GetImageHandler) //GET /images/{photo}
	}

	apiGroup := router.Group("/api")
	apiGroup.Use(func(c *gin.Context) { // 所有API禁止缓存
		c.Header("Cache-Control", "no-store")
		c.Next()
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

				imagesGroup.POST("/list", images2.ImageListHandler)                  // POST /api/v1/images/list
				imagesGroup.POST("/delete", images2.DeleteImagesHandler)             // POST /api/v1/images/delete
				imagesGroup.DELETE("/:identifier", images2.DeleteSingleImageHandler) // POST /api/v1/images/{photo}
			}

			// static token
			apiTokenGroup := v1.Group("/token")
			apiTokenGroup.Use(middleware.Authorize("jwt"))
			{
				apiTokenGroup.POST("/create", key2.CreateStaticToken) // POST /api/v1/token/create
				apiTokenGroup.GET("/all", key2.GetToken)              // GET /api/v1/token/all

				apiTokenGroup.POST("/:id/disable", key2.DisableToken) // POST /api/v1/token/{id}/disable
				apiTokenGroup.POST("/:id/enable", key2.EnableToken)   // POST /api/v1/token/{id}/enable
				apiTokenGroup.DELETE("/:id", key2.RevokeToken)        // DELETE /api/v1/token/{id}
			}
		}
	}

	return router, cleanup
}

// StartServer 创建 http.Server
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
