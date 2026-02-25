package images

import (
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

	width := h.parseThumbnailWidth(c)

	// 使用 Service 层获取图片并检查权限
	image, err := h.imageService.GetImageByIdentifier(identifier)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, "Image not found")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if !h.imageService.CheckImagePermission(image, userID) {
		common.RespondError(c, http.StatusForbidden, "This image is private")
		return
	}

	ctx := c.Request.Context()
	settings, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		// return 原图
		h.serveOriginalImage(c, image)
		return
	}

	if !settings.ThumbnailEnabled {
		h.serveOriginalImage(c, image)
		return
	}

	if !settings.IsValidWidth(width) {
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
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", "image/webp")

	ctx := c.Request.Context()
	reader, err := storage.GetDefault().GetWithContext(ctx, result.StoragePath)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}

	c.Header("Content-Length", strconv.FormatInt(result.FileSize, 10))

	c.DataFromReader(http.StatusOK, result.FileSize, result.MIMEType, reader, nil)
}
