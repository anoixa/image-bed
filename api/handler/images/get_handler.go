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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
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

// GetImage 获取图片
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

	imageData, err := h.cacheHelper.GetCachedImageData(c.Request.Context(), identifier)
	if err == nil {
		h.serveImageData(c, image, imageData)
		return
	}

	h.streamAndCacheImage(c, image)
}

// fetchImageMetadata 查询图片信息
func (h *Handler) fetchImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
	var image models.Image

	// 缓存命中
	if err := h.cacheHelper.GetCachedImage(ctx, identifier, &image); err == nil {
		return &image, nil
	}

	resultChan := imageGroup.DoChan(identifier, func() (interface{}, error) {
		imagePtr, err := images.GetImageByIdentifier(identifier)
		if err != nil {
			if isTransientError(err) {
				return nil, ErrTemporaryFailure
			}
			return nil, err
		}

		// 写入缓存（异步）
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

// streamAndCacheImage 流式处理
func (h *Handler) streamAndCacheImage(c *gin.Context, image *models.Image) {
	storageProvider, err := h.storageFactory.Get(image.StorageDriver)
	if err != nil {
		log.Printf("Failed to get storage provider for driver '%s': %v", image.StorageDriver, err)
		common.RespondError(c, http.StatusInternalServerError, "Error retrieving storage provider")
		return
	}

	imageStream, err := storageProvider.GetWithContext(c.Request.Context(), image.Identifier)
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
	enableImageCaching := cfg.Server.CacheConfig.EnableImageCaching
	maxCacheSize := cfg.Server.CacheConfig.MaxImageCacheSize
	shouldCache := enableImageCaching && image.FileSize > 0 &&
		(maxCacheSize == 0 || image.FileSize <= maxCacheSize)

	rs, isSeeker := imageStream.(io.ReadSeeker)

	if c.IsAborted() {
		return
	}

	// 如果文件太大或禁用缓存，直接流式传输
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

	// 缓存逻辑：仅对小文件启用
	if isSeeker {
		// 小文件直接读入内存缓存
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

	maxBufferSize := int64(maxCacheSize)
	if maxBufferSize <= 0 {
		maxBufferSize = 10 * 1024 * 1024 // 默认 10MB
	}

	// 使用 LimitReader 限制读取大小
	limitedReader := io.LimitReader(imageStream, maxBufferSize)
	var buffer bytes.Buffer

	// 先尝试读取到缓冲区（带大小限制）
	n, copyErr := io.Copy(&buffer, limitedReader)
	if copyErr != nil && !isBrokenPipe(copyErr) {
		log.Printf("Failed to buffer image stream for '%s': %v", image.Identifier, copyErr)
		return
	}

	// 如果实际大小超过限制，改用流式传输
	if n >= maxBufferSize || (image.FileSize > 0 && n < image.FileSize) {
		log.Printf("Image %s too large for buffering (%d bytes), streaming directly", image.Identifier, n)
		// 由于已经读了部分内容，需要组合传输
		multiReader := io.MultiReader(bytes.NewReader(buffer.Bytes()), imageStream)
		if image.FileSize > 0 {
			c.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
		}
		_ = h.serveImageStreamManualCopy(c, image, multiReader)
		return
	}

	// 完整读取，可以缓存
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

// serveImageStream 流式处理
func (h *Handler) serveImageStream(c *gin.Context, image *models.Image, stream io.Reader) error {
	h.setCommonHeaders(c, image)

	if c.Writer.Status() == http.StatusNotModified {
		return nil
	}

	if rs, ok := stream.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, image.OriginalName, image.UpdatedAt, rs)
		return nil
	}

	return h.serveImageStreamManualCopy(c, image, stream)
}

// serveImageStreamManualCopy 使用缓冲池优化传输
func (h *Handler) serveImageStreamManualCopy(c *gin.Context, image *models.Image, stream io.Reader) error {
	h.setCommonHeaders(c, image)
	if c.Writer.Status() == http.StatusNotModified {
		return nil
	}

	if c.Writer.Header().Get("Content-Length") == "" && image.FileSize > 0 {
		c.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
	}

	c.Writer.WriteHeader(http.StatusOK)

	// 使用共享缓冲池优化传输
	buf := pool.SharedBufferPool.Get().([]byte)
	defer pool.SharedBufferPool.Put(buf)

	_, err := io.CopyBuffer(c.Writer, stream, buf)
	return err
}

// setCommonHeaders 响应头处理
func (h *Handler) setCommonHeaders(c *gin.Context, image *models.Image) {
	if _, ok := c.GetQuery("download"); ok {
		safeName := sanitizeFilename(image.OriginalName)
		asciiName := toASCII(safeName)
		utf8Name := url.PathEscape(safeName)

		contentDisposition := fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, utf8Name)
		c.Header("Content-Disposition", contentDisposition)
	}

	etag := fmt.Sprintf("W/\"%x\"", image.UpdatedAt.UnixNano())

	// If-None-Match 处理
	if inm := c.GetHeader("If-None-Match"); inm != "" {
		if matchETag(inm, etag) {
			c.Header("ETag", etag)
			c.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
			c.Status(http.StatusNotModified)
			c.Abort()
			return
		}
	}

	// 只有在没有 ETag 的情况下才检查 If-Modified-Since
	if c.GetHeader("If-None-Match") == "" {
		ifModifiedSince := c.GetHeader("If-Modified-Since")
		if ifModifiedSince != "" {
			t, err := time.Parse(http.TimeFormat, ifModifiedSince)
			if err == nil && !image.UpdatedAt.After(t) {
				c.Header("ETag", etag)
				c.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
				c.Status(http.StatusNotModified)
				c.Abort()
				return
			}
		}
	}

	c.Header("Content-Type", image.MimeType)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("ETag", etag)
	c.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
}

func (h *Handler) handleMetadataError(c *gin.Context, identifier string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.RespondError(c, http.StatusNotFound, "Image not found")
	} else if errors.Is(err, ErrTemporaryFailure) {
		log.Printf("Transient error for identifier '%s'. Client should retry.", identifier)
		common.RespondError(c, http.StatusServiceUnavailable, "Service is temporarily busy, please try again.")
	} else {
		log.Printf("Database error when fetching identifier '%s': %v", utils.SanitizeLogMessage(identifier), err)
		common.RespondError(c, http.StatusInternalServerError, "Error retrieving image information")
	}
}

func toASCII(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r > unicode.MaxASCII {
			sb.WriteRune('_')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// sanitizeFilename 文件名判断
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	re := regexp.MustCompile(`[^\w\-.]`)
	safe := re.ReplaceAllString(name, "_")
	if safe == "" {
		safe = "file"
	}
	return safe
}

// isTransientError 错误检查
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}

	return false
}

// isBrokenPipe 跳过 BrokenPipe 错误
func isBrokenPipe(err error) bool {
	return err != nil && (errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET))
}

// matchETag 比较 If-None-Match header 与生成的 etag
func matchETag(headerVal, etag string) bool {
	headerVal = strings.TrimSpace(headerVal)
	if headerVal == "*" {
		return true
	}
	parts := strings.Split(headerVal, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == etag {
			return true
		}
		if p == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}
