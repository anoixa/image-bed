package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var (
	imageGroup        singleflight.Group
	fileDownloadGroup singleflight.Group
	metaFetchTimeout  = 30 * time.Second
)

var (
	ErrTemporaryFailure = errors.New("temporary failure, should be retried")
)


// GetImage 获取图片（支持格式协商）
func (h *Handler) GetImage(c *gin.Context) {
	identifier := c.Param("identifier")
	if identifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required")
		return
	}

	image, err := h.fetchImageMetadata(c.Request.Context(), identifier)
	if err != nil {
		h.handleMetadataError(c, utils.SanitizeLogMessage(identifier), err)
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

	// 格式协商
	acceptHeader := c.GetHeader("Accept")
	variantResult, err := h.variantService.SelectBestVariant(context.Background(), image, acceptHeader)
	if err != nil {
		log.Printf("[GetImage] Format negotiation failed for %s: %v", identifier, err)
		h.serveOriginalImage(c, image)
		return
	}

	if !variantResult.IsOriginal && variantResult.Variant == nil {
		go h.converter.TriggerWebPConversion(image)

		h.serveOriginalImage(c, image)
		return
	}

	if variantResult.IsOriginal {
		h.serveOriginalImage(c, image)
	} else {
		h.serveVariantImage(c, image, variantResult)
	}
}

// serveOriginalImage 提供原图
func (h *Handler) serveOriginalImage(c *gin.Context, image *models.Image) {
	identifier := image.Identifier

	// 检查缓存
	imageData, err := h.cacheHelper.GetCachedImageData(c.Request.Context(), identifier)
	if err == nil {
		h.serveImageData(c, image, imageData)
		return
	}

	// 本地存储
	if opener, ok := storage.GetDefault().(storage.FileOpener); ok {
		if h.serveBySendfile(c, image, opener) {
			return
		}
	}

	// 远程存储
	data, err := h.fetchFromRemote(identifier)
	if err != nil {
		log.Printf("[serveOriginal] Failed to get image %s: %v", identifier, err)
		common.RespondError(c, http.StatusNotFound, "Image file not found")
		return
	}

	h.serveImageData(c, image, data)
}

// fetchFromRemote 从远程存储获取图片数据（带 singleflight 防击穿）
func (h *Handler) fetchFromRemote(identifier string) ([]byte, error) {
	v, err, _ := fileDownloadGroup.Do(identifier, func() (interface{}, error) {
		// 双重检查缓存
		if data, err := h.cacheHelper.GetCachedImageData(context.Background(), identifier); err == nil {
			return data, nil
		}

		// 从存储获取
		stream, err := storage.GetDefault().GetWithContext(context.Background(), identifier)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closer, ok := stream.(io.Closer); ok {
				_ = closer.Close()
			}
		}()

		// 读取数据到内存
		data, err := io.ReadAll(stream)
		if err != nil {
			return nil, err
		}

		// 异步缓存
		go func() {
			if h.cacheHelper != nil {
				_ = h.cacheHelper.CacheImageData(context.Background(), identifier, data)
			}
		}()

		return data, nil
	})

	if err != nil {
		return nil, err
	}

	return v.([]byte), nil
}

// serveBySendfile 使用 sendfile 零拷贝传输
func (h *Handler) serveBySendfile(c *gin.Context, image *models.Image, opener storage.FileOpener) bool {
	file, err := opener.OpenFile(c.Request.Context(), image.Identifier)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return false
	}

	// 设置响应头
	contentType := image.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	if image.OriginalName != "" {
		asciiName := toASCII(image.OriginalName)
		rfc5987Name := url.QueryEscape(image.OriginalName)
		c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"; filename*=UTF-8''%s`, asciiName, rfc5987Name))
	}

	http.ServeContent(c.Writer, c.Request, image.OriginalName, stat.ModTime(), file)

	// 异步预热缓存
	go h.warmCache(image)

	return true
}

// serveVariantImage 提供格式变体
func (h *Handler) serveVariantImage(c *gin.Context, img *models.Image, result *image.VariantResult) {
	variant := result.Variant
	identifier := variant.Identifier

	// 尝试从缓存获取
	imageData, err := h.cacheHelper.GetCachedImageData(context.Background(), identifier)
	if err == nil {
		h.serveVariantData(c, img, result, imageData)
		return
	}

	// 判断是否为本地存储
	if opener, ok := storage.GetDefault().(storage.FileOpener); ok {
		file, err := opener.OpenFile(c.Request.Context(), identifier)
		if err == nil {
			defer func() { _ = file.Close() }()
			if stat, err := file.Stat(); err == nil {
				c.Header("Content-Type", result.MIMEType)
				c.Header("Cache-Control", "public, max-age=2592000, immutable")
				c.Header("Vary", "Accept")
				http.ServeContent(c.Writer, c.Request, "", stat.ModTime(), file)
				return
			}
		}
	}

	// 远程存储
	data, err := h.fetchFromRemote(identifier)
	if err != nil {
		log.Printf("[serveVariant] Failed to get variant %s: %v", identifier, err)
		h.serveOriginalImage(c, img)
		return
	}

	h.serveVariantData(c, img, result, data)
}

// serveVariantData 提供变体数据
func (h *Handler) serveVariantData(c *gin.Context, img *models.Image, result *image.VariantResult, data []byte) {
	reader := bytes.NewReader(data)
	contentType := result.MIMEType
	if contentType == "" {
		contentType = "image/webp"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Header("Vary", "Accept")
	http.ServeContent(c.Writer, c.Request, "", time.Time{}, reader)
}

// fetchImageMetadata 查询图片信息
func (h *Handler) fetchImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
	var image models.Image

	// 缓存命中
	if err := h.cacheHelper.GetCachedImage(ctx, identifier, &image); err == nil {
		return &image, nil
	}

	resultChan := imageGroup.DoChan(identifier, func() (interface{}, error) {
		imagePtr, err := h.repo.GetImageByIdentifier(identifier)
		if err != nil {
			if isTransientError(err) {
				return nil, ErrTemporaryFailure
			}
			return nil, err
		}

		go func(img *models.Image) {
			if h.cacheHelper == nil {
				return
			}
			cacheCtx := context.Background()
			if cacheErr := h.cacheHelper.CacheImage(cacheCtx, img); cacheErr != nil {
				log.Printf("Failed to cache image metadata for '%s': %v", img.Identifier, cacheErr)
			}
		}(imagePtr)

		return imagePtr, nil
	})

	select {
	case result := <-resultChan:
		if result.Err != nil {
			if errors.Is(result.Err, ErrTemporaryFailure) {
				imageGroup.Forget(identifier)
			}
			return nil, result.Err
		}
		return result.Val.(*models.Image), nil
	case <-time.After(metaFetchTimeout):
		imageGroup.Forget(identifier)
		return nil, ErrTemporaryFailure
	}
}

// handleMetadataError 处理元数据查询错误
func (h *Handler) handleMetadataError(c *gin.Context, identifier string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Image not found: %s", identifier)
		common.RespondError(c, http.StatusNotFound, "Image not found")
		return
	}

	if errors.Is(err, ErrTemporaryFailure) {
		log.Printf("Temporary failure fetching image metadata for '%s': %v", identifier, err)
		common.RespondError(c, http.StatusServiceUnavailable, "Service temporarily unavailable, please retry")
		return
	}

	log.Printf("Failed to fetch image metadata for '%s': %v", identifier, err)
	common.RespondError(c, http.StatusInternalServerError, "Error retrieving image")
}

// isTransientError 检查是否为临时错误
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	timeoutPatterns := []string{
		"timeout",
		"deadline exceeded",
		"connection refused",
		"connection reset",
		"temporary",
		"i/o timeout",
		"context deadline exceeded",
		"connection timed out",
		"no such host",
		"network is unreachable",
	}

	for _, pattern := range timeoutPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}
	return false
}

// streamAndCacheImage 流式处理（保留用于兼容）
func (h *Handler) streamAndCacheImage(c *gin.Context, image *models.Image) {
	imageStream, err := storage.GetDefault().GetWithContext(c.Request.Context(), image.Identifier)
	if err != nil {
		log.Printf("WARN: File for identifier '%s' not found in storage, but exists in DB. Error: %v", utils.SanitizeLogMessage(image.Identifier), err)
		common.RespondError(c, http.StatusNotFound, "Image file not found in storage")
		return
	}

	defer func() {
		if closer, ok := imageStream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	cfg := config.Get()
	enableImageCaching := cfg.CacheEnableImageCaching
	maxCacheSizeMB := cfg.CacheMaxImageCacheSizeMB
	maxCacheSize := maxCacheSizeMB * 1024 * 1024
	shouldCache := enableImageCaching && image.FileSize > 0 && (maxCacheSizeMB == 0 || image.FileSize <= maxCacheSize)

	if c.IsAborted() {
		return
	}

	// if文件太大或禁用缓存fallback到流式传输
	if !shouldCache {
		if image.FileSize > 0 {
			c.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
		}
		_ = h.serveImageStreamManualCopy(c, image, imageStream)
		return
	}

	data, readErr := io.ReadAll(imageStream)
	if readErr != nil {
		if !isBrokenPipe(readErr) {
			log.Printf("Failed to read image data for %s: %v", image.Identifier, readErr)
		}
		return
	}

	// 异步缓存
	go func(id string, d []byte) {
		if h.cacheHelper == nil {
			return
		}
		ctx := context.Background()
		if cacheErr := h.cacheHelper.CacheImageData(ctx, id, d); cacheErr != nil {
			log.Printf("Failed to cache image data for '%s': %v", id, cacheErr)
		}
	}(image.Identifier, data)

	h.serveImageStream(c, image, bytes.NewReader(data))
}

// serveImageData 直接提供图片数据
func (h *Handler) serveImageData(c *gin.Context, image *models.Image, data []byte) {
	reader := bytes.NewReader(data)
	h.serveImageStream(c, image, reader)
}

// serveImageStream 流式提供图片
func (h *Handler) serveImageStream(c *gin.Context, image *models.Image, reader io.ReadSeeker) {
	contentType := image.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	if image.OriginalName != "" {
		asciiName := toASCII(image.OriginalName)
		rfc5987Name := url.QueryEscape(image.OriginalName)
		if asciiName == image.OriginalName {
			c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, asciiName))
		} else {
			c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"; filename*=UTF-8''%s`, asciiName, rfc5987Name))
		}
	}
	http.ServeContent(c.Writer, c.Request, image.OriginalName, time.Time{}, reader)
}

// serveImageStreamManualCopy 手动拷贝流式图片
func (h *Handler) serveImageStreamManualCopy(c *gin.Context, image *models.Image, reader io.Reader) error {
	contentType := image.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")

	if image.OriginalName != "" {
		asciiName := toASCII(image.OriginalName)
		rfc5987Name := url.QueryEscape(image.OriginalName)
		if asciiName == image.OriginalName {
			c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, asciiName))
		} else {
			c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"; filename*=UTF-8''%s`, asciiName, rfc5987Name))
		}
	}

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)
	buf := *bufPtr

	_, err := io.CopyBuffer(c.Writer, reader, buf)
	if err != nil {
		if isBrokenPipe(err) {
			return nil
		}
		return err
	}
	return nil
}

// isBrokenPipe 检查是否为断开的连接错误
func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	// 检查常见的连接断开错误
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "write tcp") && strings.Contains(errStr, "reset")
}

// toASCII 将字符串转换为 ASCII 表示（非 ASCII 字符转为下划线）
func toASCII(s string) string {
	var result []rune
	for _, r := range s {
		if r > unicode.MaxASCII {
			result = append(result, '_')
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

