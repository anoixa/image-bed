package images

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

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

	if strings.ContainsAny(identifier, "/\\") || strings.Contains(identifier, "..") {
		common.RespondError(c, http.StatusBadRequest, "Invalid image identifier")
		return
	}

	width := h.parseThumbnailWidth(c)

	ctx := c.Request.Context()

	image, err := h.readService.GetImageMetadata(ctx, identifier)
	if err != nil {
		common.RespondError(c, http.StatusNotFound, "Image not found")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if !h.readService.CheckImagePermission(image, userID) {
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
		webpResult, webpExists, _ := h.thumbnailService.GetWebPVariant(ctx, image)
		if webpExists {
			middleware.RecordImageThumbnailResponse()
			h.serveThumbnailImage(c, image, webpResult)
			return
		}
		h.serveOriginalImage(c, image)
		return
	}

	middleware.RecordImageThumbnailResponse()
	h.serveThumbnailImage(c, image, thumbnailResult)
}

// serveThumbnailImage 提供缩略图（支持直链模式）
func (h *Handler) serveThumbnailImage(c *gin.Context, image *models.Image, result *image.ThumbnailResult) {
	if directURL := h.getVariantDirectURLIfPossible(c, image, result.StoragePath, result.FileSize); directURL != "" {
		middleware.RecordImageDirectRedirect()
		c.Header("Cache-Control", config.CacheControlPublic)
		c.Redirect(http.StatusFound, directURL)
		return
	}

	provider, err := h.getStorageProvider(image.StorageConfigID)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}

	if opener, ok := provider.(storage.FileOpener); ok {
		if h.serveThumbnailBySendfile(c, image, result, opener) {
			return
		}
	}

	if streamer, ok := provider.(storage.StreamProvider); ok {
		if h.serveThumbnailByStreaming(c, image, result, streamer) {
			return
		}
	}

	stream, err := provider.GetWithContext(c.Request.Context(), result.StoragePath)
	if err != nil {
		h.serveOriginalImage(c, image)
		return
	}
	defer func() {
		if closer, ok := stream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	middleware.RecordImageReaderResponse()
	h.serveReadSeekerContent(c, result.Identifier, result.MIMEType, result.FileHash, stream, true, cacheControlForImage(image.IsPublic))
}

func (h *Handler) serveThumbnailByStreaming(c *gin.Context, image *models.Image, result *image.ThumbnailResult, streamer storage.StreamProvider) bool {
	if checkETag(c, result.FileHash) {
		return true
	}

	c.Header("Cache-Control", cacheControlForImage(image.IsPublic))
	c.Header("Content-Type", result.MIMEType)
	c.Header("X-Content-Type-Options", "nosniff")

	_, err := streamer.StreamTo(c.Request.Context(), result.StoragePath, c.Writer)
	if err != nil {
		return utils.IsClientDisconnect(err)
	}
	middleware.RecordImageStreamResponse()
	return true
}

func (h *Handler) serveThumbnailBySendfile(c *gin.Context, image *models.Image, result *image.ThumbnailResult, opener storage.FileOpener) bool {
	if checkETag(c, result.FileHash) {
		return true
	}

	file, err := opener.OpenFile(c.Request.Context(), result.StoragePath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return false
	}

	c.Header("Cache-Control", cacheControlForImage(image.IsPublic))
	c.Header("Content-Type", result.MIMEType)
	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("X-Content-Type-Options", "nosniff")

	http.ServeContent(c.Writer, c.Request, result.Identifier, stat.ModTime().Truncate(time.Second), file)
	middleware.RecordImageSendfileResponse()
	return true
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
