package core

import (
	"github.com/anoixa/image-bed/utils"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/images"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/gin-contrib/cors"

	"github.com/gin-gonic/gin"
)

// 启动gin
func setupRouter() (*gin.Engine, func()) {
	if utils.CommitHash != "n/a" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// 全局中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-Api-Token"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

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
			"version": utils.Version,
			"commit":  utils.CommitHash,
		})
	})

	router.GET("/images/:identifier", images.GetImageHandler)

	apiGroup := router.Group("/api")
	apiGroup.Use(func(c *gin.Context) { // 所有API禁止缓存
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	{
		authGroup := apiGroup.Group("/auth")
		authGroup.Use(authRateLimiter.Middleware())
		{
			authGroup.POST("/login", api.LoginHandler)
			authGroup.POST("/refresh", api.RefreshTokenHandler)
			authGroup.POST("/logout", api.LogoutHandler)
		}

		webV1 := apiGroup.Group("/v1/web")
		webV1.Use(generalRateLimiter.Middleware())
		webV1.Use(middleware.Auth())
		{
			webV1.POST("/upload", images.UploadImageHandler)
		}

		clientV1 := apiGroup.Group("/v1/client")
		clientV1.Use(generalRateLimiter.Middleware())
		clientV1.Use(middleware.StaticTokenAuth())
		{
			clientV1.POST("/upload", images.UploadImageHandler)
		}
	}

	return router, cleanup
}

// StartServer 创建 http.Server
func StartServer() (*http.Server, func()) {
	cfg := config.Get()
	router, clean := setupRouter()

	srv := &http.Server{
		Addr:    cfg.ServerAddr(),
		Handler: router,
	}

	return srv, clean
}
