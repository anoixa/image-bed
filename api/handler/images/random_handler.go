package images

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/internal/random"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

// randomService 随机图片服务实例
var randomService *random.Service

// InitRandomService 初始化随机图片服务
func (h *Handler) InitRandomService() {
	if randomService == nil && h.configManager != nil {
		randomService = random.NewService(h.configManager)
	}
}

// getRandomSourceAlbum 获取配置的随机图源相册ID
func (h *Handler) getRandomSourceAlbum() uint {
	h.InitRandomService()
	if randomService != nil {
		return randomService.GetSourceAlbum()
	}
	return 0
}

// RandomImageQuery 随机图片查询参数
type RandomImageQuery struct {
	Format    string `form:"format"`     // 返回格式: json 或直接图片
	AlbumID   uint   `form:"album_id"`   // 指定相册
	MinWidth  int    `form:"min_width"`  // 最小宽度
	MinHeight int    `form:"min_height"` // 最小高度
	MaxWidth  int    `form:"max_width"`  // 最大宽度
	MaxHeight int    `form:"max_height"` // 最大高度
}

// RandomImage 随机图片API
func (h *Handler) RandomImage(c *gin.Context) {
	var query RandomImageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid query parameters")
		return
	}

	// 构建筛选条件
	filter := &images.RandomImageFilter{
		MinWidth:  query.MinWidth,
		MinHeight: query.MinHeight,
		MaxWidth:  query.MaxWidth,
		MaxHeight: query.MaxHeight,
	}

	// 优先使用请求参数中的相册ID，否则使用配置的推荐相册
	if query.AlbumID > 0 {
		filter.AlbumID = &query.AlbumID
	} else {
		sourceAlbumID := h.getRandomSourceAlbum()
		if sourceAlbumID > 0 {
			filter.AlbumID = &sourceAlbumID
		}
	}

	// 获取随机图片（包含格式变体协商）
	acceptHeader := c.GetHeader("Accept")
	result, err := h.imageService.GetRandomImageWithVariant(c.Request.Context(), filter, acceptHeader)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	// JSON模式：返回元数据
	if query.Format == "json" {
		h.respondRandomJSON(c, result)
		return
	}

	// 图片模式：直接输出图片内容
	if result.IsOriginal {
		h.serveOriginalImage(c, result.Image)
	} else {
		variantResult := &image.VariantResult{
			IsOriginal:  false,
			Variant:     result.Variant,
			MIMEType:    result.MIMEType,
			Identifier:  result.Variant.Identifier,
			StoragePath: result.Variant.StoragePath,
		}
		h.serveVariantImage(c, result.Image, variantResult)
	}
}

// respondRandomJSON 返回随机图片的JSON元数据
func (h *Handler) respondRandomJSON(c *gin.Context, result *image.ImageResultDTO) {
	img := result.Image
	response := gin.H{
		"id":         img.ID,
		"identifier": img.Identifier,
		"url":        utils.BuildImageURL(h.baseURL, img.Identifier),
		"width":      img.Width,
		"height":     img.Height,
		"size":       img.FileSize,
		"mime_type":  img.MimeType,
		"is_public":  img.IsPublic,
		"created_at": img.CreatedAt,
	}

	// 如果有格式变体，添加变体信息
	if !result.IsOriginal && result.Variant != nil {
		response["variant"] = gin.H{
			"identifier": result.Variant.Identifier,
			"format":     result.Variant.Format,
			"url":        utils.BuildImageURL(h.baseURL, result.Variant.Identifier),
		}
		response["mime_type"] = result.MIMEType
	}

	common.RespondSuccess(c, response)
}

// GetRandomSourceAlbum 获取随机图片源相册配置
func (h *Handler) GetRandomSourceAlbum(c *gin.Context) {
	albumID := h.getRandomSourceAlbum()
	common.RespondSuccess(c, gin.H{
		"album_id": albumID,
	})
}

// SetRandomSourceAlbumRequest 设置随机图源相册请求
type SetRandomSourceAlbumRequest struct {
	AlbumID uint `json:"album_id" binding:"required"`
}

// SetRandomSourceAlbum 设置随机图片源相册
func (h *Handler) SetRandomSourceAlbum(c *gin.Context) {
	var req SetRandomSourceAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request: album_id is required")
		return
	}

	h.InitRandomService()
	if randomService == nil {
		common.RespondError(c, http.StatusInternalServerError, "Random service not initialized")
		return
	}

	if err := randomService.SetSourceAlbum(req.AlbumID); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	common.RespondSuccess(c, gin.H{
		"album_id": req.AlbumID,
		"message":  "Random source album updated successfully",
	})
}
