package images

import (
	"io"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/services/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
)

// GetThumbnail 获取缩略图
func (h *Handler) GetThumbnail(c *gin.Context) {
	identifier := c.Param("identifier")
	if identifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required")
		return
	}

	// 解析宽度参数
	width := h.parseThumbnailWidth(c)

	// 获取原图元数据
	image, err := h.repo.GetImageByIdentifier(identifier)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, "Image not found")
		return
	}

	// 检查私有图片权限
	if !image.IsPublic {
		userID := c.GetUint(middleware.ContextUserIDKey)
		if userID == 0 || userID != image.UserID {
			common.RespondError(c, http.StatusForbidden, "This image is private")
			return
		}
	}

	// 获取缩略图配置
	ctx := c.Request.Context()
	settings, err := h.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		// 配置获取失败，返回原图
		h.serveOriginalImage(c, image)
		return
	}

	if !settings.Enabled {
		// 缩略图功能未启用，返回原图
		h.serveOriginalImage(c, image)
		return
	}

	// 检查是否为有效尺寸
	if !models.IsValidThumbnailWidth(width, settings.Sizes) {
		// 使用默认尺寸
		width = 600
	}

	// 获取或生成缩略图
	thumbnailResult, exists, err := h.thumbnailService.EnsureThumbnail(ctx, image, width)
	if err != nil {
		// 出错时回退到原图
		h.serveOriginalImage(c, image)
		return
	}

	if !exists {
		// 缩略图正在生成中，返回原图
		h.serveOriginalImage(c, image)
		return
	}

	// 返回缩略图
	h.serveThumbnailImage(c, image, thumbnailResult)
}

// parseThumbnailWidth 解析缩略图宽度参数
func (h *Handler) parseThumbnailWidth(c *gin.Context) int {
	widthStr := c.DefaultQuery("width", "300")
	width, err := strconv.Atoi(widthStr)
	if err != nil || width <= 0 {
		return 300
	}
	return width
}

// serveThumbnailImage 提供缩略图
func (h *Handler) serveThumbnailImage(c *gin.Context, image *models.Image, result *image.ThumbnailResult) {
	// 设置缓存头
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", "image/webp")

	// 获取缩略图数据
	ctx := c.Request.Context()
	reader, err := storage.GetDefault().GetWithContext(ctx, result.Identifier)
	if err != nil {
		// 存储获取失败，返回原图
		h.serveOriginalImage(c, image)
		return
	}

	// 获取内容长度
	size, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}
	reader.Seek(0, io.SeekStart)

	// 设置内容长度
	c.Header("Content-Length", strconv.FormatInt(size, 10))

	// 返回图片数据
	c.DataFromReader(http.StatusOK, size, result.MIMEType, reader, nil)
}
