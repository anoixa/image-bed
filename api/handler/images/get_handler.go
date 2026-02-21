package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	imageGroup       singleflight.Group
	metaFetchTimeout = 30 * time.Second
)

var (
	ErrTemporaryFailure = errors.New("temporary failure, should be retried")
)

// rangeInfo range 请求信息
type rangeInfo struct {
	start  int64
	end    int64
	length int64
}

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
	// 检查缓存
	imageData, err := h.cacheHelper.GetCachedImageData(c.Request.Context(), image.Identifier)
	if err == nil {
		h.serveImageData(c, image, imageData)
		return
	}

	if h.serveBySendfile(c, image) {
		return
	}

	// 回退到普通流式传输
	h.streamAndCacheImage(c, image)
}

// serveBySendfile 使用 sendfile 零拷贝传输
func (h *Handler) serveBySendfile(c *gin.Context, image *models.Image) bool {
	opener, ok := storage.GetDefault().(storage.FileOpener)
	if !ok {
		return false
	}

	file, err := opener.OpenFile(c.Request.Context(), image.Identifier)
	if err != nil {
		return false
	}
	defer file.Close()

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

	// 使用 http.ServeContent 自动处理 Range 和 sendfile
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

	imageStream, err := storage.GetDefault().GetWithContext(context.Background(), identifier)
	if err != nil {
		log.Printf("[serveVariant] Failed to get variant %s: %v", identifier, err)

		h.serveOriginalImage(c, img)
		return
	}
	defer func() {
		if closer, ok := imageStream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	rs, isSeeker := imageStream.(io.ReadSeeker)
	if isSeeker {
		data, readErr := io.ReadAll(rs)
		if readErr != nil {
			log.Printf("[serveVariant] Failed to read variant %s: %v", identifier, readErr)
			h.serveOriginalImage(c, img)
			return
		}

		// 异步缓存
		go func(id string, d []byte) {
			if h.cacheHelper == nil {
				return
			}
			ctx := context.Background()
			if cacheErr := h.cacheHelper.CacheImageData(ctx, id, d); cacheErr != nil {
				log.Printf("[serveVariant] Failed to cache variant %s: %v", id, cacheErr)
			}
		}(identifier, data)

		h.serveVariantData(c, img, result, data)
		return
	}

	// 非 seeker，直接流式传输
	c.Header("Content-Type", result.MIMEType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Header("Vary", "Accept")
	c.DataFromReader(http.StatusOK, variant.FileSize, result.MIMEType, imageStream, nil)
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

	// 处理 range 请求
	c.Writer.Header().Set("Accept-Ranges", "bytes")
	c.Writer.Header().Set("Content-Length", strconv.Itoa(len(data)))

	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}

	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		reader.Seek(0, io.SeekStart)
		c.DataFromReader(http.StatusOK, int64(len(data)), contentType, reader, nil)
		return
	}

	// 解析 range 请求
	ranges := h.parseRangeHeader(rangeHeader, int64(len(data)))
	if len(ranges) == 0 {
		reader.Seek(0, io.SeekStart)
		c.DataFromReader(http.StatusOK, int64(len(data)), contentType, reader, nil)
		return
	}

	// 支持单 range
	r := ranges[0]
	reader.Seek(r.start, io.SeekStart)
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, len(data)))
	c.Header("Content-Length", strconv.FormatInt(r.length, 10))
	c.Status(http.StatusPartialContent)
	io.CopyN(c.Writer, reader, r.length)
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

// streamAndCacheImage 流式处理
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

	rs, isSeeker := imageStream.(io.ReadSeeker)

	if c.IsAborted() {
		return
	}

	// if文件太大或禁用缓存fallback到流式传输
	if !shouldCache {
		if isSeeker {
			_ = h.serveImageStream(c, image, rs)
		} else {
			if image.FileSize > 0 {
				c.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
			}
			_ = h.serveImageStreamManualCopy(c, image, imageStream)
		}
		return
	}

	if isSeeker {
		data, readErr := io.ReadAll(rs)
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

		_ = h.serveImageStream(c, image, bytes.NewReader(data))
		return
	}

	maxBufferSize := maxCacheSize
	if maxBufferSize <= 0 {
		maxBufferSize = 10 * 1024 * 1024 // 默认 10MB
	}

	// 使用 LimitReader 限制读取大小
	limitedReader := io.LimitReader(imageStream, maxBufferSize)
	var buffer bytes.Buffer

	// 尝试读取到缓冲区
	n, copyErr := io.Copy(&buffer, limitedReader)
	if copyErr != nil && !isBrokenPipe(copyErr) {
		log.Printf("Failed to buffer image stream for '%s': %v", image.Identifier, copyErr)
		return
	}

	// 如果实际大小超过限制fallback流式传输
	if n >= maxBufferSize || (image.FileSize > 0 && n < image.FileSize) {
		log.Printf("Image %s too large for buffering (%d bytes), streaming directly", image.Identifier, n)

		multiReader := io.MultiReader(bytes.NewReader(buffer.Bytes()), imageStream)
		if image.FileSize > 0 {
			c.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
		}
		_ = h.serveImageStreamManualCopy(c, image, multiReader)
		return
	}

	data := buffer.Bytes()
	go func(id string, d []byte) {
		if h.cacheHelper == nil {
			return
		}
		ctx := context.Background()
		if cacheErr := h.cacheHelper.CacheImageData(ctx, id, d); cacheErr != nil {
			log.Printf("Failed to cache image data for '%s': %v", id, cacheErr)
		}
	}(image.Identifier, data)

	_ = h.serveImageStream(c, image, bytes.NewReader(data))
}

// serveImageData 直接提供图片数据
func (h *Handler) serveImageData(c *gin.Context, image *models.Image, data []byte) {
	reader := bytes.NewReader(data)
	_ = h.serveImageStream(c, image, reader)
}

// serveImageStream 流式提供图片
func (h *Handler) serveImageStream(c *gin.Context, image *models.Image, reader io.ReadSeeker) error {
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

	// 处理 range 请求
	c.Writer.Header().Set("Accept-Ranges", "bytes")
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(image.FileSize, 10))

	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return nil
	}

	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		reader.Seek(0, io.SeekStart)
		_, err := io.Copy(c.Writer, reader)
		return err
	}

	ranges := h.parseRangeHeader(rangeHeader, image.FileSize)
	if len(ranges) == 0 {
		reader.Seek(0, io.SeekStart)
		_, err := io.Copy(c.Writer, reader)
		return err
	}

	// 支持单 range
	r := ranges[0]
	reader.Seek(r.start, io.SeekStart)
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, image.FileSize))
	c.Header("Content-Length", strconv.FormatInt(r.length, 10))
	c.Status(http.StatusPartialContent)
	_, err := io.CopyN(c.Writer, reader, r.length)
	return err
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

	buf := pool.SharedBufferPool.Get().([]byte)
	defer pool.SharedBufferPool.Put(buf)

	_, err := io.CopyBuffer(c.Writer, reader, buf)
	if err != nil {
		if isBrokenPipe(err) {
			return nil
		}
		return err
	}
	return nil
}

// parseRangeHeader 解析 HTTP Range 请求头
func (h *Handler) parseRangeHeader(rangeHeader string, fileSize int64) []rangeInfo {
	const prefix = "bytes="
	if !strings.HasPrefix(rangeHeader, prefix) {
		return nil
	}

	ranges := strings.Split(rangeHeader[len(prefix):], ",")
	var result []rangeInfo

	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		parts := strings.Split(r, "-")
		if len(parts) != 2 {
			continue
		}

		var start, end int64
		var err error

		if parts[0] == "" {
			length, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil || length <= 0 {
				continue
			}
			start = fileSize - length
			if start < 0 {
				start = 0
			}
			end = fileSize - 1
		} else {
			start, err = strconv.ParseInt(parts[0], 10, 64)
			if err != nil || start < 0 || start >= fileSize {
				continue
			}

			if parts[1] == "" {
				end = fileSize - 1
			} else {
				end, err = strconv.ParseInt(parts[1], 10, 64)
				if err != nil || end < start || end >= fileSize {
					continue
				}
			}
		}

		result = append(result, rangeInfo{
			start:  start,
			end:    end,
			length: end - start + 1,
		})
	}

	return result
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

var svgMagicRegex = regexp.MustCompile(`(?i)^\s*(?:<\?xml[^>]*>\s*)?<svg`)

// isSVG 检查文件是否为 SVG 格式
func isSVG(data []byte) bool {
	return svgMagicRegex.Match(data)
}

// isSafeFileName 检查文件名是否安全
func isSafeFileName(name string) bool {
	// 检查是否包含路径分隔符
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	// 检查是否为隐藏文件
	if strings.HasPrefix(name, ".") {
		return false
	}
	return true
}

// isClientDisconnected 检查客户端是否已断开连接
func isClientDisconnected(err error) bool {
	if err == nil {
		return false
	}

	// 检查网络错误
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || !netErr.Temporary() {
			return true
		}
	}

	errStr := strings.ToLower(err.Error())
	disconnectedPatterns := []string{
		"broken pipe",
		"connection reset by peer",
		"client disconnected",
		"context canceled",
		"write tcp",
	}

	for _, pattern := range disconnectedPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// 检查文件系统错误
	if errors.Is(err, os.ErrClosed) {
		return true
	}

	return false
}
