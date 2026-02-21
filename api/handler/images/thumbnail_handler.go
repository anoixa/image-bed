package images

import (
	"io"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
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
		// return 原图
		h.serveOriginalImage(c, image)
		return
	}

	if !settings.Enabled {
		h.serveOriginalImage(c, image)
		return
	}

	// 检查是否为有效尺寸
	if !models.IsValidThumbnailWidth(width, settings.Sizes) {
		width = 600
	}

	thumbnailResult, exists, err := h.thumbnailService.EnsureThumbnail(ctx, image, width)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}

	if !exists {
		h.serveOriginalImage(c, image)
		return
	}

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

	ctx := c.Request.Context()
	reader, err := storage.GetDefault().GetWithContext(ctx, result.Identifier)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}

	size, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}
	_, _ = reader.Seek(0, io.SeekStart)

	c.Header("Content-Length", strconv.FormatInt(size, 10))

	// 返回图片数据
	c.DataFromReader(http.StatusOK, size, result.MIMEType, reader, nil)
}
