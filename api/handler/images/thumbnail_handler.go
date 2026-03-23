package images

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

var thumbnailGroup singleflight.Group

// GetThumbnail 获取缩略图
// @Summary      Get image thumbnail
// @Description  Retrieve a thumbnail version of an image with specified width
// @Tags         images
// @Accept       json
// @Produce      image/webp
// @Param        identifier  path      string  true   "Image identifier"
// @Param        width       query     int     false  "Thumbnail width (default: 300)"
// @Success      200         {file}    binary   "Thumbnail image data"
// @Failure      400         {object}  common.Response  "Invalid identifier"
// @Failure      403         {object}  common.Response  "Private image, access denied"
// @Failure      404         {object}  common.Response  "Image not found"
// @Failure      500         {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /thumbnails/{identifier} [get]
func (h *Handler) GetThumbnail(c *gin.Context) {
	identifier := c.Param("identifier")
	if identifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required")
		return
	}

	width := h.parseThumbnailWidth(c)

	ctx := c.Request.Context()

	image, err := h.imageService.GetImageMetadata(ctx, identifier)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, "Image not found")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if !h.imageService.CheckImagePermission(image, userID) {
		common.RespondError(c, http.StatusForbidden, "This image is private")
		return
	}
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

// serveThumbnailImage 提供缩略图（支持直链模式）
func (h *Handler) serveThumbnailImage(c *gin.Context, image *models.Image, result *image.ThumbnailResult) {
	// 检查缩略图是否可以使用直链（使用缩略图自己的路径）
	if directURL := h.getVariantDirectURLIfPossible(c, image, result.StoragePath); directURL != "" {
		c.Header("Cache-Control", config.CacheControlPublic)
		c.Redirect(http.StatusFound, directURL)
		return
	}

	c.Header("Cache-Control", config.CacheControlPublic)
	c.Header("Content-Type", config.ContentTypeWebP)

	v, err, _ := thumbnailGroup.Do(result.StoragePath, func() (any, error) {
		provider := h.getStorageProvider(image.StorageConfigID)
		if provider == nil {
			return nil, fmt.Errorf("storage provider not available")
		}
		stream, err := provider.GetWithContext(c.Request.Context(), result.StoragePath)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closer, ok := stream.(io.Closer); ok {
				_ = closer.Close()
			}
		}()
		return io.ReadAll(stream)
	})
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}

	data := v.([]byte)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Data(http.StatusOK, result.MIMEType, data)
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
