package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"golang.org/x/sync/singleflight"
)

var fileDownloadGroup singleflight.Group

const privateImageCacheControl = "private, no-store"

func cacheControlForImage(isPublic bool) string {
	if isPublic {
		return config.CacheControlPublic
	}
	return privateImageCacheControl
}

// checkETag 检查客户端缓存是否有效
// 如果客户端发送的 If-None-Match 与当前 ETag 匹配，返回 true 并写入 304 响应
func checkETag(c *gin.Context, etag string) bool {
	if etag == "" {
		return false
	}

	etag = normalizeETag(etag)
	c.Header("ETag", etag)

	if matchesIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return true
	}

	return false
}

func normalizeETag(etag string) string {
	etag = strings.TrimSpace(etag)
	if !strings.HasPrefix(etag, "\"") && !strings.HasPrefix(etag, "W/\"") {
		return "\"" + etag + "\""
	}
	return etag
}

func matchesIfNoneMatch(headerValue, currentETag string) bool {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return false
	}
	if headerValue == "*" {
		return true
	}

	currentOpaque := trimWeakETag(currentETag)
	for _, candidate := range strings.Split(headerValue, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == "*" {
			return true
		}
		if trimWeakETag(candidate) == currentOpaque {
			return true
		}
	}

	return false
}

func trimWeakETag(etag string) string {
	etag = strings.TrimSpace(etag)
	if strings.HasPrefix(etag, "W/") {
		return strings.TrimSpace(strings.TrimPrefix(etag, "W/"))
	}
	return etag
}

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

	// 防止路径穿越攻击
	if strings.ContainsAny(identifier, "/\\") || strings.Contains(identifier, "..") {
		common.RespondError(c, http.StatusBadRequest, "Invalid image identifier")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	acceptHeader := c.GetHeader("Accept")

	result, err := h.readService.GetImageWithVariant(c.Request.Context(), identifier, acceptHeader, userID)
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
	if h.cacheHelper == nil {
		common.RespondError(c, http.StatusInternalServerError, "Cache not initialized")
		return
	}

	// 检查是否可以使用直链
	if directURL := h.getDirectURLIfPossible(c, image); directURL != "" {
		c.Header("Cache-Control", config.CacheControlPublic)
		c.Redirect(http.StatusFound, directURL)
		return
	}

	storagePath := image.StoragePath

	// 使用图片指定的 StorageConfigID 获取正确的存储 provider
	provider, err := h.getStorageProvider(image.StorageConfigID)
	if err != nil {
		imageHandlerLog.Errorf("serveOriginalImage failed to get storage provider for image %s (StorageConfigID=%d)",
			image.Identifier, image.StorageConfigID)
		common.RespondError(c, http.StatusInternalServerError, "Storage provider not available")
		return
	}

	if imageData, ok := h.getOrPopulateImageDataCache(c.Request.Context(), provider, image.Identifier, storagePath); ok {
		h.serveImageData(c, image, imageData)
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

	stream, err := provider.GetWithContext(c.Request.Context(), storagePath)
	if err != nil {
		imageHandlerLog.Errorf("serveOriginal failed to get image %s (path: %s): %v", utils.SanitizeLogMessage(image.Identifier), storagePath, err)
		common.RespondError(c, http.StatusNotFound, "Image file not found")
		return
	}
	defer func() {
		if closer, ok := stream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	h.serveReadSeekerContent(c, image.Identifier, image.MimeType, image.FileHash, stream, false, cacheControlForImage(image.IsPublic))
}

// getDirectURLIfPossible 尝试获取直链 URL
func (h *Handler) getDirectURLIfPossible(c *gin.Context, img *models.Image) string {
	return h.getVariantDirectURLIfPossible(c, img, img.StoragePath)
}

// getVariantDirectURLIfPossible 尝试获取变体的直链 URL
func (h *Handler) getVariantDirectURLIfPossible(c *gin.Context, img *models.Image, storagePath string) string {
	// 私有图片不支持直链
	if !img.IsPublic {
		return ""
	}

	// 获取存储提供者
	provider, err := h.getStorageProvider(img.StorageConfigID)
	if err != nil {
		return ""
	}

	directProvider, ok := provider.(storage.DirectURLProvider)
	if !ok {
		return ""
	}

	globalMode := h.getGlobalTransferMode(c.Request.Context())

	if directProvider.ShouldProxy(img.IsPublic, globalMode) {
		return ""
	}

	// 获取直链 URL
	return directProvider.GetDirectURL(storagePath)
}

// getGlobalTransferMode 获取全局转发模式
func (h *Handler) getGlobalTransferMode(ctx context.Context) storage.TransferMode {
	v, err, _ := fileDownloadGroup.Do("global_transfer_mode", func() (any, error) {
		mode := h.configManager.GetGlobalTransferMode(ctx)
		return mode, nil
	})
	if err != nil {
		return storage.TransferModeAuto
	}
	return v.(storage.TransferMode)
}

func (h *Handler) serveByStreaming(c *gin.Context, img *models.Image, streamer storage.StreamProvider) bool {
	// 检查 ETag 缓存
	if checkETag(c, img.FileHash) {
		return true
	}

	c.Header("Cache-Control", cacheControlForImage(img.IsPublic))
	c.Header("Content-Type", img.MimeType)

	_, err := streamer.StreamTo(c.Request.Context(), img.StoragePath, c.Writer)
	if err != nil {
		if utils.IsClientDisconnect(err) {
			return true
		}
		imageHandlerLog.Errorf("serveByStreaming failed to stream image %s (path: %s): %v", utils.SanitizeLogMessage(img.Identifier), img.StoragePath, err)
		return false
	}
	return true
}

// fetchFromRemoteWithProvider 从指定存储提供者获取图片数据
// getStorageProvider 根据图片的 StorageConfigID 获取对应的存储 provider
func (h *Handler) getStorageProvider(storageConfigID uint) (storage.Provider, error) {
	if storageConfigID == 0 {
		provider := storage.GetDefault()
		if provider == nil {
			return nil, fmt.Errorf("default storage provider not configured")
		}
		return provider, nil
	}

	provider, err := storage.GetByID(storageConfigID)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

// serveBySendfile 使用 sendfile 零拷贝传输
func (h *Handler) serveBySendfile(c *gin.Context, img *models.Image, opener storage.FileOpener) bool {
	// 检查 ETag 缓存（在打开文件前检查，避免不必要的 IO）
	if checkETag(c, img.FileHash) {
		return true
	}

	file, err := opener.OpenFile(c.Request.Context(), img.StoragePath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	c.Header("Cache-Control", cacheControlForImage(img.IsPublic))
	c.Header("Content-Type", img.MimeType)

	http.ServeContent(c.Writer, c.Request, img.Identifier, time.Time{}, file)
	return true
}

// serveImageData 从内存提供图片数据
func (h *Handler) serveImageData(c *gin.Context, img *models.Image, data []byte) {
	// 检查 ETag 缓存
	if checkETag(c, img.FileHash) {
		return
	}

	c.Header("Cache-Control", cacheControlForImage(img.IsPublic))
	c.Header("Content-Type", img.MimeType)
	c.Header("Content-Length", strconv.Itoa(len(data)))

	c.Data(http.StatusOK, img.MimeType, data)
}

// serveVariantImage 提供格式变体（支持直链模式）
func (h *Handler) serveVariantImage(c *gin.Context, img *models.Image, result *image.VariantResult) {
	// 检查变体是否可以使用直链（使用变体自己的路径）
	if directURL := h.getVariantDirectURLIfPossible(c, img, result.StoragePath); directURL != "" {
		c.Header("Cache-Control", config.CacheControlPublic)
		c.Redirect(http.StatusFound, directURL)
		return
	}

	// 使用原图指定的 StorageConfigID 获取正确的存储 provider
	provider, err := h.getStorageProvider(img.StorageConfigID)
	if err != nil {
		imageHandlerLog.Errorf("serveVariantImage failed to get storage provider for image %s (StorageConfigID=%d)",
			img.Identifier, img.StorageConfigID)
		// 降级到原图
		h.serveOriginalImage(c, img)
		return
	}

	if imageData, ok := h.getOrPopulateImageDataCache(c.Request.Context(), provider, remoteImageDataCacheKey(img.StorageConfigID, result.StoragePath), result.StoragePath); ok {
		h.serveVariantData(c, result, imageData, img.IsPublic)
		return
	}

	if opener, ok := provider.(storage.FileOpener); ok {
		if h.serveVariantBySendfile(c, result, opener, img.IsPublic) {
			return
		}
	}

	if streamer, ok := provider.(storage.StreamProvider); ok {
		if h.serveVariantByStreaming(c, result, streamer, img.IsPublic) {
			return
		}
	}

	stream, err := provider.GetWithContext(c.Request.Context(), result.StoragePath)
	if err != nil {
		imageHandlerLog.Errorf("serveVariant failed to get variant %s (path: %s): %v", utils.SanitizeLogMessage(result.Identifier), result.StoragePath, err)
		// 降级到原图
		h.serveOriginalImage(c, img)
		return
	}
	defer func() {
		if closer, ok := stream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	h.serveReadSeekerContent(c, result.Identifier, result.MIMEType, result.Variant.FileHash, stream, true, cacheControlForImage(img.IsPublic))
}

// serveVariantByStreaming 使用流式传输格式变体
func (h *Handler) serveVariantByStreaming(c *gin.Context, result *image.VariantResult, streamer storage.StreamProvider, isPublic bool) bool {
	// 使用 FileHash 作为变体 ETag
	if checkETag(c, result.Variant.FileHash) {
		return true
	}

	c.Header("Cache-Control", cacheControlForImage(isPublic))
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
func (h *Handler) serveVariantBySendfile(c *gin.Context, result *image.VariantResult, opener storage.FileOpener, isPublic bool) bool {
	// 检查 ETag 缓存（使用 FileHash 作为变体 ETag）
	if checkETag(c, result.Variant.FileHash) {
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

	c.Header("Cache-Control", cacheControlForImage(isPublic))
	c.Header("Content-Type", result.MIMEType)
	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("X-Content-Type-Options", "nosniff")

	http.ServeContent(c.Writer, c.Request, result.Identifier, stat.ModTime(), file)
	return true
}

// serveVariantData 从内存提供格式变体数据
func (h *Handler) serveVariantData(c *gin.Context, result *image.VariantResult, data []byte, isPublic bool) {
	// 使用 FileHash 作为变体 ETag
	if checkETag(c, result.Variant.FileHash) {
		return
	}

	c.Header("Cache-Control", cacheControlForImage(isPublic))
	c.Header("Content-Type", result.MIMEType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("X-Content-Type-Options", "nosniff")

	// 输出数据
	c.DataFromReader(http.StatusOK, int64(len(data)), result.MIMEType, bytes.NewReader(data), nil)
}

func remoteImageDataCacheKey(storageConfigID uint, storagePath string) string {
	return fmt.Sprintf("%d:%s", storageConfigID, storagePath)
}

func (h *Handler) shouldUseImageDataCache(provider storage.Provider) bool {
	if !h.imageDataCaching || h.cacheHelper == nil || provider == nil {
		return false
	}

	// LocalStorage 已经有 file path / sendfile 优化，没必要再查二进制缓存。
	if _, ok := provider.(storage.PathProvider); ok {
		return false
	}

	return true
}

func (h *Handler) getOrPopulateImageDataCache(ctx context.Context, provider storage.Provider, cacheKey, storagePath string) ([]byte, bool) {
	if !h.shouldUseImageDataCache(provider) {
		return nil, false
	}

	imageData, err := h.cacheHelper.GetCachedImageData(ctx, cacheKey)
	if err == nil {
		return imageData, true
	}

	imageData, ok, err := h.loadCacheableImageData(ctx, provider, storagePath)
	if err != nil {
		return nil, false
	}
	if !ok {
		return nil, false
	}

	_ = h.cacheHelper.CacheImageData(ctx, cacheKey, imageData)

	return imageData, true
}

func (h *Handler) loadCacheableImageData(ctx context.Context, provider storage.Provider, storagePath string) ([]byte, bool, error) {
	if h.cacheHelper == nil {
		return nil, false, nil
	}

	maxSize := h.cacheHelper.MaxCacheableImageSize()
	if maxSize <= 0 {
		return nil, false, nil
	}

	infoProvider, ok := provider.(storage.ObjectInfoProvider)
	if !ok {
		// Unknown remote size means a cache miss would force a full pre-read
		// and then a second download for streaming. Skip binary caching instead.
		return nil, false, nil
	}

	info, err := infoProvider.GetObjectInfo(ctx, storagePath)
	if err != nil {
		return nil, false, err
	}
	if info.Size > maxSize {
		return nil, false, nil
	}

	stream, err := provider.GetWithContext(ctx, storagePath)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closer, ok := stream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	if size, err := readRemainingReadSeekerSize(stream); err == nil && size > maxSize {
		return nil, false, nil
	}

	data, err := io.ReadAll(io.LimitReader(stream, maxSize+1))
	if err != nil {
		return nil, false, err
	}

	if int64(len(data)) > maxSize {
		return nil, false, nil
	}

	return data, true, nil
}

func readRemainingReadSeekerSize(stream io.ReadSeeker) (int64, error) {
	currentPos, err := stream.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	endPos, err := stream.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := stream.Seek(currentPos, io.SeekStart); err != nil {
		return 0, err
	}
	return endPos - currentPos, nil
}

func (h *Handler) serveReadSeekerContent(c *gin.Context, identifier, mimeType, etag string, stream io.ReadSeeker, noSniff bool, cacheControl string) {
	if checkETag(c, etag) {
		return
	}

	c.Header("Cache-Control", cacheControl)
	c.Header("Content-Type", mimeType)
	if noSniff {
		c.Header("X-Content-Type-Options", "nosniff")
	}

	http.ServeContent(c.Writer, c.Request, identifier, time.Time{}, stream)
}

// handleMetadataError 处理元数据查询错误
func (h *Handler) handleMetadataError(c *gin.Context, identifier string, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		imageHandlerLog.Errorf("Timeout fetching image metadata for '%s'", utils.SanitizeLogMessage(identifier))
		common.RespondError(c, http.StatusGatewayTimeout, "Request timeout")
		return
	}

	if errors.Is(err, image.ErrForbidden) {
		common.RespondError(c, http.StatusForbidden, "This image is private")
		return
	}

	// 临时错误返回 503
	if errors.Is(err, image.ErrTemporaryFailure) {
		imageHandlerLog.Errorf("Temporary failure fetching metadata for '%s': %v", utils.SanitizeLogMessage(identifier), err)
		common.RespondError(c, http.StatusServiceUnavailable, "Service temporarily unavailable")
		return
	}

	imageHandlerLog.Errorf("Failed to fetch image metadata for '%s': %v", utils.SanitizeLogMessage(identifier), err)
	common.RespondError(c, http.StatusNotFound, "Image not found")
}
