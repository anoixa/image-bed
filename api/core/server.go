package core

import (
	"github.com/gin-contrib/cors"
	"image-bed/api"
	"image-bed/api/images"
	"image-bed/api/middleware"
	"image-bed/config"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// 启动gin
func setupRouter() (*gin.Engine, func()) {
	//cfg := config.Get()
	//if cfg.Server.Mode == "release" {
	//	gin.SetMode(gin.ReleaseMode)
	//}

	router := gin.New()

	// 全局中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // 在生产环境中应配置为具体的前端域名
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// 速率限制
	authRateLimiter := middleware.NewIPRateLimiter(0.5, 5, 10*time.Minute) // rps=0.5 -> 30 req/min
	generalRateLimiter := middleware.NewIPRateLimiter(10, 20, 10*time.Minute)

	cleanup := func() {
		authRateLimiter.StopCleanup()
		generalRateLimiter.StopCleanup()
	}

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "^_^")
	})

	apiGroup := router.Group("/api")
	{
		apiGroup.Use(func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			c.Next()
		})

		authGroup := apiGroup.Group("/auth")
		authGroup.Use(authRateLimiter.Middleware()) // 应用严格的限流策略
		{
			authGroup.POST("/login", api.LoginHandler)
			authGroup.POST("/refresh", api.RefreshTokenHandler)
		}

		v1 := apiGroup.Group("/v1")
		v1.Use(generalRateLimiter.Middleware())
		v1.Use(middleware.Auth())
		{
			v1.POST("/upload", images.UploadImageHandler)
			v1.POST("/logout", api.LogoutHandler)
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
