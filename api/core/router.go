package core

import (
	"net/http"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/handler/admin"
	handlerAlbums "github.com/anoixa/image-bed/api/handler/albums"
	handlerImages "github.com/anoixa/image-bed/api/handler/images"
	"github.com/anoixa/image-bed/api/handler/key"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	svcAlbums "github.com/anoixa/image-bed/internal/albums"
	"github.com/anoixa/image-bed/internal/auth"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/gin-gonic/gin"
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
	TokenManager     *auth.TokenManager
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
	// 基础路由
	registerBasicRoutes(router, deps)

	// 公共接口路由
	registerPublicRoutes(router, deps)

	// API 路由
	registerAPIRoutes(router, deps)
}

// registerBasicRoutes 注册基础路由
func registerBasicRoutes(router *gin.Engine, deps *RouterDependencies) {
	healthHandler := NewHealthHandler(deps.DB)
	router.GET("/health", healthHandler.Handle)

	router.GET("/version", func(context *gin.Context) {
		common.RespondSuccess(context, gin.H{
			"version": deps.ServerVersion.Version,
			"commit":  deps.ServerVersion.CommitHash,
		})
	})

	router.GET("/metrics", func(context *gin.Context) {
		context.JSON(http.StatusOK, middleware.GetMetrics())
	})
}

// registerPublicRoutes 注册公共接口路由
func registerPublicRoutes(router *gin.Engine, deps *RouterDependencies) {
	cfg := deps.Config
	baseURL := getBaseURL(cfg)
	uploadMaxBatchTotalMB := 500 // 默认值
	if cfg != nil {
		uploadMaxBatchTotalMB = cfg.UploadMaxBatchTotalMB
	}

	// 创建处理器
	imageHandler := handlerImages.NewHandler(deps.CacheProvider, deps.Repositories.ImagesRepo, deps.DB, deps.Converter, deps.ConfigManager, cfg, baseURL, uploadMaxBatchTotalMB)

	// 公共图片访问
	publicGroup := router.Group("/images")
	publicGroup.Use(deps.ImageRateLimiter.Middleware())
	{
		publicGroup.GET("/:identifier", imageHandler.GetImage)
	}

	// 缩略图公共访问
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
	uploadMaxBatchTotalMB := 500 // 默认值
	if cfg != nil {
		uploadMaxBatchTotalMB = cfg.UploadMaxBatchTotalMB
	}

	imageHandler := handlerImages.NewHandler(deps.CacheProvider, deps.Repositories.ImagesRepo, deps.DB, deps.Converter, deps.ConfigManager, cfg, baseURL, uploadMaxBatchTotalMB)
	albumService := svcAlbums.NewService(deps.Repositories.AlbumsRepo)
	albumHandler := handlerAlbums.NewHandler(albumService, deps.CacheProvider, baseURL)
	albumImageHandler := handlerAlbums.NewAlbumImageHandler(albumService, deps.Repositories.ImagesRepo, deps.CacheProvider, cfg)
	keyService := auth.NewKeyService(deps.Repositories.KeysRepo)
	keyHandler := key.NewHandler(keyService)
	loginHandler := api.NewLoginHandlerWithService(deps.LoginService, cfg)

	apiGroup := router.Group("/api")
	apiGroup.Use(func(context *gin.Context) {
		context.Header("Cache-Control", "no-store")
		context.Next()
	})
	{
		// 认证路由
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
			imagesGroup.Use(middleware.Authorize("jwt", "static_token"))
			{
				imagesGroup.POST("/upload", imageHandler.UploadImage)
				imagesGroup.POST("/uploads", imageHandler.UploadImages)
				imagesGroup.POST("", imageHandler.ListImages)
				imagesGroup.POST("/delete", imageHandler.DeleteImages)
				imagesGroup.DELETE("/:identifier", imageHandler.DeleteSingleImage)
				imagesGroup.PATCH("/:identifier/visibility", imageHandler.UpdateImageVisibility)
			}

			// Static Token
			apiTokenGroup := v1.Group("/token")
			apiTokenGroup.Use(middleware.Authorize("jwt"))
			{
				apiTokenGroup.POST("", keyHandler.CreateStaticToken)
				apiTokenGroup.GET("", keyHandler.GetToken)
				apiTokenGroup.POST("/:id/disable", keyHandler.DisableToken)
				apiTokenGroup.POST("/:id/enable", keyHandler.EnableToken)
				apiTokenGroup.DELETE("/:id", keyHandler.RevokeToken)
			}

			// Albums
			albumsGroup := v1.Group("/albums")
			albumsGroup.Use(middleware.Authorize("jwt"))
			{
				albumsGroup.GET("", albumHandler.ListAlbumsHandler)
				albumsGroup.POST("", albumHandler.CreateAlbumHandler)
				albumsGroup.GET("/:id", albumHandler.GetAlbumDetailHandler)
				albumsGroup.PUT("/:id", albumHandler.UpdateAlbumHandler)
				albumsGroup.DELETE("/:id", albumHandler.DeleteAlbumHandler)
				albumsGroup.POST("/:id/images", albumImageHandler.AddImagesToAlbumHandler)
				albumsGroup.DELETE("/:id/images/:imageId", albumImageHandler.RemoveImageFromAlbumHandler)
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
	configHandler := admin.NewConfigHandler(deps.ConfigManager)
	adminGroup := v1.Group("/admin")
	adminGroup.Use(middleware.Authorize("jwt"))
	adminGroup.Use(middleware.RequireRole("admin"))
	{
		// Configs
		configsGroup := adminGroup.Group("/configs")
		{
			configsGroup.GET("", configHandler.ListConfigs)
			configsGroup.POST("", configHandler.CreateConfig)
			configsGroup.GET("/:id", configHandler.GetConfig)
			configsGroup.PUT("/:id", configHandler.UpdateConfig)
			configsGroup.DELETE("/:id", configHandler.DeleteConfig)
			configsGroup.POST("/:id/test", configHandler.TestConfig)
			configsGroup.POST("/:id/default", configHandler.SetDefaultConfig)
			configsGroup.POST("/:id/enable", configHandler.EnableConfig)
			configsGroup.POST("/:id/disable", configHandler.DisableConfig)
		}

		adminGroup.GET("/storage/providers", configHandler.ListStorageProviders)
		adminGroup.POST("/storage/reload/:id", configHandler.ReloadStorageConfig)

		conversionHandler := admin.NewConversionHandler(deps.ConfigManager)
		adminGroup.GET("/conversion", conversionHandler.GetConfig)
		adminGroup.PUT("/conversion", conversionHandler.UpdateConfig)
	}
}
