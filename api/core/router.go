package core

import (
	"strings"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/handler/admin"
	handlerAlbums "github.com/anoixa/image-bed/api/handler/albums"
	handlerDashboard "github.com/anoixa/image-bed/api/handler/dashboard"
	handlerImages "github.com/anoixa/image-bed/api/handler/images"
	"github.com/anoixa/image-bed/api/handler/key"
	handlerSystem "github.com/anoixa/image-bed/api/handler/system"
	handlerUser "github.com/anoixa/image-bed/api/handler/user"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	dashboardRepo "github.com/anoixa/image-bed/database/repo/dashboard"
	svcAlbums "github.com/anoixa/image-bed/internal/albums"
	"github.com/anoixa/image-bed/internal/auth"
	svcDashboard "github.com/anoixa/image-bed/internal/dashboard"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	svcUser "github.com/anoixa/image-bed/internal/user"
	"github.com/anoixa/image-bed/public"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

// getBaseURL 从配置获取基础 URL
func getBaseURL(cfg *config.Config) string {
	if cfg != nil && cfg.ServerDomain != "" {
		return cfg.ServerDomain
	}
	return ""
}

// RouterDependencies 路由注册依赖
type RouterDependencies struct {
	DB               *gorm.DB
	Repositories     *Repositories
	ConfigManager    *configSvc.Manager
	Converter        *imageSvc.Converter
	JWTService       *auth.JWTService
	LoginService     *auth.LoginService
	AuthRateLimiter  *middleware.IPRateLimiter
	APIRateLimiter   *middleware.IPRateLimiter
	ImageRateLimiter *middleware.IPRateLimiter
	CacheProvider    cache.Provider
	ServerVersion    ServerVersion
	Config           *config.Config
}

// RegisterRoutes 注册所有路由
func RegisterRoutes(router *gin.Engine, deps *RouterDependencies) {
	registerBasicRoutes(router, deps)
	registerPublicRoutes(router, deps)
	registerAPIRoutes(router, deps)

	if deps.Config != nil && deps.Config.ServeFrontend {
		registerStaticRoutes(router)
	}
}

// registerBasicRoutes 注册基础路由
func registerBasicRoutes(router *gin.Engine, deps *RouterDependencies) {
	// System Routes
	systemGroup := router.Group("/system")
	{
		systemHandler := handlerSystem.NewHandler()
		healthHandler := handlerSystem.NewHealthHandler(deps.DB, storage.GetDefault())

		systemGroup.Any("/health", healthHandler.Handle)
		systemGroup.GET("/version", systemHandler.GetVersion)
		systemGroup.GET("/metrics", systemHandler.GetMetrics)

		authSystemGroup := systemGroup.Group("")
		authSystemGroup.Use(middleware.CombinedAuth())
		authSystemGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
		authSystemGroup.GET("/status", systemHandler.GetStatus)
	}

	// Swagger 文档路由（开发环境可用）
	if !config.IsProduction() {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
}

// registerPublicRoutes 注册公共接口路由
func registerPublicRoutes(router *gin.Engine, deps *RouterDependencies) {
	cfg := deps.Config
	baseURL := getBaseURL(cfg)
	uploadMaxBatchTotalMB := 500
	if cfg != nil {
		uploadMaxBatchTotalMB = cfg.UploadMaxBatchTotalMB
	}

	imageHandler := handlerImages.NewHandler(deps.CacheProvider, deps.Repositories.ImagesRepo, deps.DB, deps.Converter, deps.ConfigManager, cfg, baseURL, uploadMaxBatchTotalMB, storage.GetDefault())

	// 公共图片访问
	publicGroup := router.Group("/images")
	publicGroup.Use(deps.ImageRateLimiter.Middleware())
	{
		publicGroup.GET("/random", imageHandler.RandomImage)
		publicGroup.GET("/:identifier", imageHandler.GetImage)
	}

	thumbnailGroup := router.Group("/thumbnails")
	thumbnailGroup.Use(deps.ImageRateLimiter.Middleware())
	{
		thumbnailGroup.GET("/:identifier", imageHandler.GetThumbnail)
	}

}

// registerAPIRoutes 注册 API 路由
func registerAPIRoutes(router *gin.Engine, deps *RouterDependencies) {
	cfg := deps.Config
	baseURL := getBaseURL(cfg)
	uploadMaxBatchTotalMB := 500
	if cfg != nil {
		uploadMaxBatchTotalMB = cfg.UploadMaxBatchTotalMB
	}

	imageHandler := handlerImages.NewHandler(deps.CacheProvider, deps.Repositories.ImagesRepo, deps.DB, deps.Converter, deps.ConfigManager, cfg, baseURL, uploadMaxBatchTotalMB, storage.GetDefault())
	albumService := svcAlbums.NewService(deps.Repositories.AlbumsRepo)
	albumHandler := handlerAlbums.NewHandler(albumService, deps.CacheProvider, baseURL)
	albumImageHandler := handlerAlbums.NewAlbumImageHandler(albumService, deps.Repositories.ImagesRepo, deps.CacheProvider, cfg)
	keyService := auth.NewKeyService(deps.Repositories.KeysRepo)
	keyHandler := key.NewHandler(keyService)
	loginHandler := api.NewLoginHandlerWithService(deps.LoginService, cfg)

	dashboardRepository := dashboardRepo.NewRepository(deps.DB)
	dashboardService := svcDashboard.NewService(dashboardRepository, deps.CacheProvider)
	dashboardHandler := handlerDashboard.NewHandler(dashboardService)

	userService := svcUser.NewService(deps.Repositories.AccountsRepo)
	userHandler := handlerUser.NewHandler(userService)

	apiGroup := router.Group("/api")
	apiGroup.Use(func(context *gin.Context) {
		context.Header("Cache-Control", config.CacheControlNoStore)
		context.Next()
	})
	{
		authGroup := apiGroup.Group("/auth")
		authGroup.Use(deps.AuthRateLimiter.Middleware())
		{
			authGroup.POST("/login", loginHandler.LoginHandlerFunc)
			authGroup.POST("/refresh", loginHandler.RefreshTokenHandlerFunc)
			authGroup.POST("/logout", loginHandler.LogoutHandlerFunc)
		}

		v1 := apiGroup.Group("/v1")
		v1.Use(deps.APIRateLimiter.Middleware())
		v1.Use(middleware.CombinedAuth())
		{
			// Images
			imagesGroup := v1.Group("/images")
			imagesGroup.Use(middleware.Authorize(middleware.AllowAllAuth...))
			{
				imagesGroup.POST("/upload", imageHandler.UploadImage)
				imagesGroup.POST("/uploads", imageHandler.UploadImages)
				imagesGroup.POST("", imageHandler.ListImages)
				imagesGroup.POST("/delete", imageHandler.DeleteImages)
				imagesGroup.DELETE("/:identifier", imageHandler.DeleteSingleImage)
				imagesGroup.PUT("/:identifier/visibility", imageHandler.UpdateImageVisibility)
			}

			// User
			userGroup := v1.Group("/user")
			userGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
			{
				userGroup.POST("/password", userHandler.ChangePassword)
			}

			// Static Token
			apiTokenGroup := v1.Group("/token")
			apiTokenGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
			{
				apiTokenGroup.POST("", keyHandler.CreateStaticToken)
				apiTokenGroup.GET("", keyHandler.GetToken)
				apiTokenGroup.POST("/:id/disable", keyHandler.DisableToken)
				apiTokenGroup.POST("/:id/enable", keyHandler.EnableToken)
				apiTokenGroup.DELETE("/:id", keyHandler.RevokeToken)
			}

			// Albums
			albumsGroup := v1.Group("/albums")
			albumsGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
			{
				albumsGroup.GET("", albumHandler.ListAlbumsHandler)
				albumsGroup.POST("", albumHandler.CreateAlbumHandler)
				albumsGroup.GET("/:id", albumHandler.GetAlbumDetailHandler)
				albumsGroup.PUT("/:id", albumHandler.UpdateAlbumHandler)
				albumsGroup.DELETE("/:id", albumHandler.DeleteAlbumHandler)
				albumsGroup.POST("/:id/images", albumImageHandler.AddImagesToAlbumHandler)
				albumsGroup.DELETE("/:id/images/:imageId", albumImageHandler.RemoveImageFromAlbumHandler)
				albumsGroup.POST("/:id/images/remove", albumImageHandler.RemoveImagesFromAlbumHandler)
			}

			dashboardGroup := v1.Group("/dashboard")
			dashboardGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
			{
				dashboardGroup.GET("/stats", dashboardHandler.GetStats)
				dashboardGroup.POST("/stats/refresh", dashboardHandler.RefreshStats)
			}

			// Admin
			if deps.ConfigManager != nil {
				registerAdminRoutes(v1, deps)
			}
		}
	}
}

// registerAdminRoutes 注册管理员路由
func registerAdminRoutes(v1 *gin.RouterGroup, deps *RouterDependencies) {
	cfg := deps.Config
	baseURL := getBaseURL(cfg)
	uploadMaxBatchTotalMB := 500
	if cfg != nil {
		uploadMaxBatchTotalMB = cfg.UploadMaxBatchTotalMB
	}
	imageHandler := handlerImages.NewHandler(deps.CacheProvider, deps.Repositories.ImagesRepo, deps.DB, deps.Converter, deps.ConfigManager, cfg, baseURL, uploadMaxBatchTotalMB, storage.GetDefault())

	configHandler := admin.NewConfigHandler(deps.ConfigManager, deps.Repositories.ImagesRepo)
	adminGroup := v1.Group("/admin")
	adminGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
	adminGroup.Use(middleware.RequireRole(middleware.RoleAdmin))
	{
		// Configs
		configsGroup := adminGroup.Group("/configs")
		{
			configsGroup.GET("", configHandler.ListConfigs)
			configsGroup.POST("", configHandler.CreateConfig)
			configsGroup.POST("/:id/test", configHandler.TestConfig)
			configsGroup.POST("/:id/default", configHandler.SetDefaultConfig)
			configsGroup.POST("/:id/enable", configHandler.EnableConfig)
			configsGroup.POST("/:id/disable", configHandler.DisableConfig)
			configsGroup.GET("/:id", configHandler.GetConfig)
			configsGroup.PUT("/:id", configHandler.UpdateConfig)
			configsGroup.DELETE("/:id", configHandler.DeleteConfig)
		}

		adminGroup.GET("/storage/providers", configHandler.ListStorageProviders)
		adminGroup.POST("/storage/reload/:id", configHandler.ReloadStorageConfig)

		conversionHandler := admin.NewConversionHandler(deps.ConfigManager)
		adminGroup.GET("/conversion", conversionHandler.GetConfig)
		adminGroup.PUT("/conversion", conversionHandler.UpdateConfig)

		// 随机图片源相册配置
		adminGroup.GET("/random-source-album", imageHandler.GetRandomSourceAlbum)
		adminGroup.POST("/random-source-album", imageHandler.SetRandomSourceAlbum)

		// 全局转发模式配置
		adminGroup.GET("/transfer-mode", configHandler.GetGlobalTransferMode)
		adminGroup.POST("/transfer-mode", configHandler.SetGlobalTransferMode)
	}
}

// registerStaticRoutes 注册静态文件路由
func registerStaticRoutes(router *gin.Engine) {
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 跳过 API 路径
		if isStaticAPIPath(path) {
			c.JSON(404, gin.H{"error": "Not found"})
			return
		}

		// 尝试打开请求的文件
		filePath := strings.TrimPrefix(path, "/")
		if filePath == "" {
			filePath = "index.html"
		}

		// 检查文件是否存在且不是目录（避免重定向问题）
		if filePath != "index.html" && public.Exists(filePath) && !public.Exists(filePath+"/index.html") {
			c.FileFromFS(filePath, public.DistFS)
			return
		}

		// 对于明确的静态文件请求（如 favicon.ico, robots.txt 等），如果不存在则返回 404
		if isStaticAssetFile(filePath) {
			c.JSON(404, gin.H{"error": "Not found"})
			return
		}

		content, err := public.ReadFile("index.html")
		if err != nil {
			c.String(500, "Failed to load index.html")
			return
		}
		c.Data(200, "text/html; charset=utf-8", content)
	})
}

// isStaticAPIPath 检查路径是否为 API 路径
func isStaticAPIPath(p string) bool {
	apiPaths := []string{
		"/api/",
		"/images/",
		"/thumbnails/",
		"/system/",
		"/swagger/",
	}
	for _, prefix := range apiPaths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// isStaticAssetFile 检查是否为明确的静态文件请求
// 这些文件如果不存在应该返回 404，而不是返回 index.html
func isStaticAssetFile(filePath string) bool {
	staticExtensions := []string{
		".ico", ".png", ".jpg", ".jpeg", ".gif", ".svg",
		".css", ".js", ".map",
		".woff", ".woff2", ".ttf", ".eot",
		".txt", ".xml",
	}
	for _, ext := range staticExtensions {
		if strings.HasSuffix(strings.ToLower(filePath), ext) {
			return true
		}
	}
	return false
}
