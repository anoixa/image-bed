package images

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

var fileDownloadGroup singleflight.Group

// GetImage 获取图片
// @Summary      Get image by identifier
// @Description  Retrieve an image by its unique identifier. Returns original or converted format based on Accept header
// @Tags         images
// @Accept       json
// @Produce      image/*
// @Param        identifier  path      string  true   "Image identifier"
// @Success      200         {file}    binary   "Image data"
// @Failure      400         {object}  common.Response  "Invalid identifier"
// @Failure      403         {object}  common.Response  "Private image, access denied"
// @Failure      404         {object}  common.Response  "Image not found"
// @Failure      500         {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /images/{identifier} [get]
func (h *Handler) GetImage(c *gin.Context) {
	identifier := c.Param("identifier")
	if identifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	acceptHeader := c.GetHeader("Accept")

	result, err := h.imageService.GetImageWithVariant(c.Request.Context(), identifier, acceptHeader, userID)
	if err != nil {
		if errors.Is(err, image.ErrForbidden) {
			common.RespondError(c, http.StatusForbidden, "This image is private")
			return
		}
		h.handleMetadataError(c, utils.SanitizeLogMessage(identifier), err)
		return
	}

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

// serveOriginalImage 提供原图
func (h *Handler) serveOriginalImage(c *gin.Context, image *models.Image) {
	// [DEBUG] 检查关键依赖
	if storage.GetDefault() == nil {
		utils.LogIfDevf("[DEBUG][serveOriginalImage] storage.GetDefault() is nil!")
		common.RespondError(c, http.StatusInternalServerError, "Storage not initialized")
		return
	}
	if h.cacheHelper == nil {
		utils.LogIfDevf("[DEBUG][serveOriginalImage] h.cacheHelper is nil!")
		common.RespondError(c, http.StatusInternalServerError, "Cache not initialized")
		return
	}
	// [DEBUG] 记录图片存储配置ID
	utils.LogIfDevf("[DEBUG][serveOriginalImage] image.ID=%d, StorageConfigID=%d, Identifier=%s",
		image.ID, image.StorageConfigID, image.Identifier)

	storagePath := image.StoragePath

	imageData, err := h.cacheHelper.GetCachedImageData(c.Request.Context(), image.Identifier)
	if err == nil {
		h.serveImageData(c, image, imageData)
		return
	}

	// 使用图片指定的 StorageConfigID 获取正确的存储 provider
	provider := h.getStorageProvider(image.StorageConfigID)
	if provider == nil {
		log.Printf("[serveOriginalImage] Failed to get storage provider for image %s (StorageConfigID=%d)",
			image.Identifier, image.StorageConfigID)
		common.RespondError(c, http.StatusInternalServerError, "Storage provider not available")
		return
	}

	if opener, ok := provider.(storage.FileOpener); ok {
		if h.serveBySendfile(c, image, opener) {
			return
		}
	}

	if streamer, ok := provider.(storage.StreamProvider); ok {
		if h.serveByStreaming(c, image, streamer) {
			return
		}
	}

	data, err := h.fetchFromRemoteWithProvider(storagePath, provider)
	if err != nil {
		log.Printf("[serveOriginal] Failed to get image %s (path: %s): %v", image.Identifier, storagePath, err)
		common.RespondError(c, http.StatusNotFound, "Image file not found")
		return
	}

	h.serveImageData(c, image, data)
}

func (h *Handler) serveByStreaming(c *gin.Context, img *models.Image, streamer storage.StreamProvider) bool {
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", img.MimeType)
	c.Header("ETag", "\""+img.FileHash+"\"")

	_, err := streamer.StreamTo(c.Request.Context(), img.StoragePath, c.Writer)
	if err != nil {
		if utils.IsClientDisconnect(err) {
			return true
		}
		log.Printf("[serveByStreaming] Failed to stream image %s (path: %s): %v", img.Identifier, img.StoragePath, err)
		return false
	}
	return true
}

// fetchFromRemoteWithProvider 从指定存储提供者获取图片数据
func (h *Handler) fetchFromRemoteWithProvider(storagePath string, provider storage.Provider) ([]byte, error) {
	v, err, _ := fileDownloadGroup.Do(storagePath, func() (interface{}, error) {
		if data, err := h.cacheHelper.GetCachedImageData(context.Background(), storagePath); err == nil {
			return data, nil
		}

		// 从指定的存储 provider 获取
		stream, err := provider.GetWithContext(context.Background(), storagePath)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closer, ok := stream.(io.Closer); ok {
				_ = closer.Close()
			}
		}()

		const maxImageSize = 50 * 1024 * 1024
		limitedReader := io.LimitReader(stream, maxImageSize)
		data, err := io.ReadAll(limitedReader)
		if err != nil {
			return nil, err
		}

		// 记录大图片日志
		if len(data) > 10*1024*1024 {
			log.Printf("[fetchFromRemote] Large image loaded: %d bytes, path: %s", len(data), storagePath)
		}

		if len(data) < 5*1024*1024 { // 只缓存小于 5MB 的图片
			task := func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = h.cacheHelper.CacheImageData(ctx, storagePath, data)
			}
			if pool := worker.GetGlobalPool(); pool != nil {
				pool.Submit(task)
			} else {
				go task()
			}
		}

		return data, nil
	})

	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// getStorageProvider 根据图片的 StorageConfigID 获取对应的存储 provider
// 如果指定的 provider 不存在，则返回默认存储
func (h *Handler) getStorageProvider(storageConfigID uint) storage.Provider {
	if storageConfigID == 0 {
		return storage.GetDefault()
	}

	provider, err := storage.GetByID(storageConfigID)
	if err != nil {
		log.Printf("[getStorageProvider] Storage provider ID=%d not found, falling back to default: %v", storageConfigID, err)
		return storage.GetDefault()
	}
	return provider
}

// serveBySendfile 使用 sendfile 零拷贝传输
func (h *Handler) serveBySendfile(c *gin.Context, img *models.Image, opener storage.FileOpener) bool {
	file, err := opener.OpenFile(c.Request.Context(), img.StoragePath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", img.MimeType)
	c.Header("ETag", "\""+img.FileHash+"\"")

	http.ServeContent(c.Writer, c.Request, img.Identifier, time.Time{}, file)
	return true
}

// serveImageData 从内存提供图片数据
func (h *Handler) serveImageData(c *gin.Context, img *models.Image, data []byte) {
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", img.MimeType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("ETag", "\""+img.FileHash+"\"")

	c.Data(http.StatusOK, img.MimeType, data)
}

// serveVariantImage 提供格式变体
func (h *Handler) serveVariantImage(c *gin.Context, img *models.Image, result *image.VariantResult) {
	// [DEBUG] 记录变体信息
	utils.LogIfDevf("[DEBUG][serveVariantImage] img.ID=%d, img.StorageConfigID=%d, variant.Identifier=%s",
		img.ID, img.StorageConfigID, result.Identifier)

	// 使用原图指定的 StorageConfigID 获取正确的存储 provider
	provider := h.getStorageProvider(img.StorageConfigID)
	if provider == nil {
		log.Printf("[serveVariantImage] Failed to get storage provider for image %s (StorageConfigID=%d)",
			img.Identifier, img.StorageConfigID)
		// 降级到原图
		h.serveOriginalImage(c, img)
		return
	}

	imageData, err := h.cacheHelper.GetCachedImageData(c.Request.Context(), result.StoragePath)
	if err == nil {
		h.serveVariantData(c, img, result, imageData)
		return
	}

	if opener, ok := provider.(storage.FileOpener); ok {
		if h.serveVariantBySendfile(c, img, result, opener) {
			return
		}
	}

	if streamer, ok := provider.(storage.StreamProvider); ok {
		if h.serveVariantByStreaming(c, result, streamer) {
			return
		}
	}

	stream, err := provider.GetWithContext(c.Request.Context(), result.StoragePath)
	if err != nil {
		log.Printf("[serveVariant] Failed to get variant %s (path: %s): %v", result.Identifier, result.StoragePath, err)
		// 降级到原图
		h.serveOriginalImage(c, img)
		return
	}
	defer func() {
		if closer, ok := stream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	data, err := io.ReadAll(stream)
	if err != nil {
		h.serveOriginalImage(c, img)
		return
	}

	task := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.cacheHelper.CacheImageData(ctx, result.StoragePath, data)
	}
	if pool := worker.GetGlobalPool(); pool != nil {
		pool.Submit(task)
	} else {
		go task()
	}

	h.serveVariantData(c, img, result, data)
}

// serveVariantByStreaming 使用流式传输格式变体
func (h *Handler) serveVariantByStreaming(c *gin.Context, result *image.VariantResult, streamer storage.StreamProvider) bool {
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", result.MIMEType)
	c.Header("X-Content-Type-Options", "nosniff")

	_, err := streamer.StreamTo(c.Request.Context(), result.StoragePath, c.Writer)
	if err != nil {
		// 客户端断开连接是正常情况
		return utils.IsClientDisconnect(err)
	}
	return true
}

// serveVariantBySendfile 使用 sendfile 传输格式变体
func (h *Handler) serveVariantBySendfile(c *gin.Context, img *models.Image, result *image.VariantResult, opener storage.FileOpener) bool {
	file, err := opener.OpenFile(c.Request.Context(), result.StoragePath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return false
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", result.MIMEType)
	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("X-Content-Type-Options", "nosniff")

	http.ServeContent(c.Writer, c.Request, result.Identifier, stat.ModTime(), file)
	return true
}

// serveVariantData 从内存提供格式变体数据
func (h *Handler) serveVariantData(c *gin.Context, img *models.Image, result *image.VariantResult, data []byte) {
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("Content-Type", result.MIMEType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("X-Content-Type-Options", "nosniff")

	// 输出数据
	c.DataFromReader(http.StatusOK, int64(len(data)), result.MIMEType, bytes.NewReader(data), nil)
}

// handleMetadataError 处理元数据查询错误
func (h *Handler) handleMetadataError(c *gin.Context, identifier string, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("Timeout fetching image metadata for '%s'", identifier)
		common.RespondError(c, http.StatusGatewayTimeout, "Request timeout")
		return
	}

	if errors.Is(err, image.ErrForbidden) {
		common.RespondError(c, http.StatusForbidden, "This image is private")
		return
	}

	// 临时错误返回 503
	if errors.Is(err, image.ErrTemporaryFailure) {
		log.Printf("Temporary failure fetching metadata for '%s': %v", identifier, err)
		common.RespondError(c, http.StatusServiceUnavailable, "Service temporarily unavailable")
		return
	}

	log.Printf("Failed to fetch image metadata for '%s': %v", identifier, err)
	common.RespondError(c, http.StatusNotFound, "Image not found")
}
