package images

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// getRandomSourceAlbum 获取配置的随机图源相册ID和是否包含所有公开图片的配置
func (h *Handler) getRandomSourceAlbum() (uint, bool) {
	if h.randomService != nil {
		return h.randomService.GetSourceAlbum()
	}
	return 0, false
}

// RandomImageQuery 随机图片查询参数
type RandomImageQuery struct {
	Format      string `form:"format"`        // 返回格式: json 或直接图片
	AlbumID     uint   `form:"album_id"`      // 指定相册
	MinWidth    int    `form:"min_width"`     // 最小宽度
	MinHeight   int    `form:"min_height"`    // 最小高度
	MaxWidth    int    `form:"max_width"`     // 最大宽度
	MaxHeight   int    `form:"max_height"`    // 最大高度
	RequireWebP bool   `form:"require_webp"`  // 是否只返回有WebP变体的图片
	MaxFileSize int64  `form:"max_file_size"` // 最大文件大小（字节），例如10485760表示10MB
}

// RandomImage 随机图片API
// @Summary      Get random image
// @Description  Get a random image, optionally filtered by album, dimensions, WebP availability and file size
// @Tags         images
// @Accept       json
// @Produce      image/*,application/json
// @Param        format         query     string  false  "Response format: json or image (default: image)"
// @Param        album_id       query     int     false  "Filter by album ID (0 = all public images, >0 = specific album)"
// @Param        min_width      query     int     false  "Minimum image width"
// @Param        min_height     query     int     false  "Minimum image height"
// @Param        max_width      query     int     false  "Maximum image width"
// @Param        max_height     query     int     false  "Maximum image height"
// @Param        require_webp   query     bool    false  "Only return images with WebP variant (default: false)"
// @Param        max_file_size  query     int     false  "Maximum file size in bytes (e.g., 10485760 for 10MB)"
// @Success      200  {file}    binary           "Image data (when format=image)"
// @Success      200  {object}  common.Response  "Image metadata (when format=json)"
// @Failure      400  {object}  common.Response  "Invalid query parameters"
// @Failure      204  {string}  string           "No content - no matching images found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Router       /images/random [get]
func (h *Handler) RandomImage(c *gin.Context) {
	var query RandomImageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid query parameters")
		return
	}
	if query.Format != "" && query.Format != "json" && query.Format != "image" {
		common.RespondError(c, http.StatusBadRequest, "Invalid format parameter")
		return
	}

	// 构建筛选条件
	filter := &images.RandomImageFilter{
		MinWidth:    query.MinWidth,
		MinHeight:   query.MinHeight,
		MaxWidth:    query.MaxWidth,
		MaxHeight:   query.MaxHeight,
		RequireWebP: query.RequireWebP,
		MaxFileSize: query.MaxFileSize,
	}

	albumIDRaw, hasAlbumOverride := c.GetQuery("album_id")
	if hasAlbumOverride {
		albumID, err := strconv.ParseUint(albumIDRaw, 10, 32)
		if err != nil {
			common.RespondError(c, http.StatusBadRequest, "Invalid album_id parameter")
			return
		}
		if albumID == 0 {
			filter.IncludeAllPublic = true
		} else {
			albumIDUint := uint(albumID)
			filter.AlbumID = &albumIDUint
		}
	} else {
		sourceAlbumID, includeAllPublic := h.getRandomSourceAlbum()
		if includeAllPublic {
			filter.IncludeAllPublic = true
		} else if sourceAlbumID > 0 {
			filter.AlbumID = &sourceAlbumID
		}
	}

	// 获取随机图片（包含格式变体协商）
	acceptHeader := c.GetHeader("Accept")
	result, err := h.readService.GetRandomImageWithVariant(c.Request.Context(), filter, acceptHeader)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
		utils.Errorf("[RandomImage] Failed to fetch random image: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to fetch random image")
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
	accessURL := utils.BuildImageURL(h.baseURL, img.Identifier)
	selectedURL := result.URL
	if selectedURL == "" {
		selectedURL = accessURL
	}
	response := gin.H{
		"id":           img.ID,
		"identifier":   img.Identifier,
		"url":          selectedURL,
		"original_url": accessURL,
		"width":        img.Width,
		"height":       img.Height,
		"size":         img.FileSize,
		"mime_type":    img.MimeType,
		"is_public":    img.IsPublic,
		"created_at":   img.CreatedAt,
	}

	if !result.IsOriginal && result.Variant != nil {
		response["variant"] = gin.H{
			"identifier":         result.Variant.Identifier,
			"request_identifier": img.Identifier,
			"format":             result.Variant.Format,
			"url":                selectedURL,
		}
		response["mime_type"] = result.MIMEType
	}

	common.RespondSuccess(c, response)
}

// GetRandomSourceAlbum 获取随机图片源相册配置
// @Summary      Get random source album configuration
// @Description  Get the currently configured random image source album ID and whether to include all public images
// @Tags         admin
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Success"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/random-source-album [get]
func (h *Handler) GetRandomSourceAlbum(c *gin.Context) {
	albumID, includeAllPublic := h.getRandomSourceAlbum()
	common.RespondSuccess(c, gin.H{
		"album_id":           albumID,
		"include_all_public": includeAllPublic,
	})
}

// SetRandomSourceAlbumRequest 设置随机图源相册请求
// AlbumID: 0 表示所有公开图片, >0 表示特定相册ID
type SetRandomSourceAlbumRequest struct {
	AlbumID          uint `json:"album_id"`
	IncludeAllPublic bool `json:"include_all_public"`
}

// SetRandomSourceAlbum 设置随机图片源相册
// @Summary      Set random source album configuration
// @Description  Configure the source album for random image selection. Use album_id=0 for all public images, or specify an album ID
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      SetRandomSourceAlbumRequest  true  "Random source configuration"
// @Success      200      {object}  common.Response                "Configuration updated successfully"
// @Failure      400      {object}  common.Response                "Invalid request"
// @Failure      401      {object}  common.Response                "Unauthorized"
// @Failure      500      {object}  common.Response                "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/random-source-album [post]
func (h *Handler) SetRandomSourceAlbum(c *gin.Context) {
	var req SetRandomSourceAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	if h.randomService == nil {
		common.RespondError(c, http.StatusInternalServerError, "Random service not initialized")
		return
	}

	if err := h.randomService.SetSourceAlbum(req.AlbumID, req.IncludeAllPublic); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	common.RespondSuccess(c, gin.H{
		"album_id":           req.AlbumID,
		"include_all_public": req.IncludeAllPublic,
		"message":            "Random source album updated successfully",
	})
}
