package images

import (
	"bytes"
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
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var (
	imageGroup          singleflight.Group
	ErrTemporaryFailure = errors.New("temporary failure, should be retried")
	metaFetchTimeout    = 5 * time.Second
)

func GetImageHandler(context *gin.Context) {
	identifier := context.Param("identifier")
	if identifier == "" {
		common.RespondError(context, http.StatusBadRequest, "Image identifier is required")
		return
	}

	image, err := fetchImageMetadata(identifier)
	if err != nil {
		handleMetadataError(context, identifier, err)
		return
	}

	if imageData, err := cache.GetCachedImageData(identifier); err == nil {
		serveImageData(context, image, imageData)
		return
	}

	streamAndCacheImage(context, image)
}

// fetchImageMetadata 查询图片信息
func fetchImageMetadata(identifier string) (*models.Image, error) {
	var image models.Image
	// 缓存命中
	if err := cache.GetCachedImage(identifier, &image); err == nil {
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
			if cacheErr := cache.CacheImage(img); cacheErr != nil {
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
func streamAndCacheImage(context *gin.Context, image *models.Image) {
	storageClient, err := storage.GetStorage(image.StorageDriver)
	if err != nil {
		log.Printf("Failed to get storage client for driver '%s': %v", image.StorageDriver, err)
		common.RespondError(context, http.StatusInternalServerError, "Error retrieving storage client")
		return
	}

	imageStream, err := storageClient.Get(image.Identifier)
	if err != nil {
		log.Printf("WARN: File for identifier '%s' not found in storage, but exists in DB. Error: %v", image.Identifier, err)
		common.RespondError(context, http.StatusNotFound, "Image file not found in storage")
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
	shouldCache := image.FileSize > 0 && (maxCacheSize == 0 || image.FileSize <= maxCacheSize)

	rs, isSeeker := imageStream.(io.ReadSeeker)

	// 修复：检查客户端是否已断开
	if context.IsAborted() {
		return
	}

	if shouldCache && enableImageCaching {
		if isSeeker {
			// 修复：限制最大读取大小，防止大文件 OOM
			data, readErr := io.ReadAll(io.LimitReader(rs, int64(maxCacheSize)))
			if readErr != nil {
				if !isBrokenPipe(readErr) {
					log.Printf("Failed to read image data for %s: %v", image.Identifier, readErr)
				}
				return
			}
			// 检查是否被截断
			if int64(len(data)) == maxCacheSize && int64(len(data)) < image.FileSize {
				log.Printf("Image %s exceeds max cache size, skipping cache", image.Identifier)
				_ = serveImageStream(context, image, rs)
				return
			}
			if readErr != nil {
				return
			}

			go func(id string, d []byte) {
				if cacheErr := cache.CacheImageData(id, d); cacheErr != nil {
					log.Printf("Failed to cache image data for '%s': %v", id, cacheErr)
				}
			}(image.Identifier, data)

			_ = serveImageStream(context, image, bytes.NewReader(data))
			return
		}

		var buffer bytes.Buffer
		tee := io.TeeReader(imageStream, &buffer)

		streamErr := serveImageStreamManualCopy(context, image, tee)
		if streamErr == nil && buffer.Len() > 0 {
			data := make([]byte, buffer.Len())
			copy(data, buffer.Bytes())
			go func(id string, d []byte) {
				if cacheErr := cache.CacheImageData(id, d); cacheErr != nil {
					log.Printf("Failed to cache image data for '%s': %v", id, cacheErr)
				}
			}(image.Identifier, data)
		} else if streamErr != nil && !isBrokenPipe(streamErr) {
			log.Printf("Stream ended with error, caching aborted for '%s'. Error: %v", image.Identifier, streamErr)
		}
		return
	}

	if isSeeker {
		_ = serveImageStream(context, image, rs)
		return
	}

	if image.FileSize > 0 {
		context.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
	}
	_ = serveImageStreamManualCopy(context, image, imageStream)
}

// serveImageData
func serveImageData(context *gin.Context, image *models.Image, data []byte) {
	reader := bytes.NewReader(data)
	_ = serveImageStream(context, image, reader)
}

// serveImageStream 流式处理
func serveImageStream(context *gin.Context, image *models.Image, stream io.Reader) error {
	setCommonHeaders(context, image)

	if context.Writer.Status() == http.StatusNotModified {
		return nil
	}

	if rs, ok := stream.(io.ReadSeeker); ok {
		http.ServeContent(context.Writer, context.Request, image.OriginalName, image.UpdatedAt, rs)
		return nil
	}

	return serveImageStreamManualCopy(context, image, stream)
}

// serveImageStreamManualCopy
func serveImageStreamManualCopy(context *gin.Context, image *models.Image, stream io.Reader) error {
	setCommonHeaders(context, image)
	if context.Writer.Status() == http.StatusNotModified {
		return nil
	}

	if context.Writer.Header().Get("Content-Length") == "" && image.FileSize > 0 {
		context.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
	}

	context.Writer.WriteHeader(http.StatusOK)
	_, err := io.Copy(context.Writer, stream)
	return err
}

// setCommonHeaders 响应头处理
func setCommonHeaders(context *gin.Context, image *models.Image) {
	if _, ok := context.GetQuery("download"); ok {
		safeName := sanitizeFilename(image.OriginalName)
		asciiName := toASCII(safeName)
		utf8Name := url.PathEscape(safeName)

		contentDisposition := fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, utf8Name)
		context.Header("Content-Disposition", contentDisposition)
	}

	etag := fmt.Sprintf("W/\"%x\"", image.UpdatedAt.UnixNano())

	// If-None-Match 处理
	if inm := context.GetHeader("If-None-Match"); inm != "" {
		if matchETag(inm, etag) {
			context.Header("ETag", etag)
			context.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
			context.Status(http.StatusNotModified)
			context.Abort()
			return
		}
	}

	// 只有在没有 ETag 的情况下才检查 If-Modified-Since
	// If-Modified-Since 处理
	if context.GetHeader("If-None-Match") == "" {
		ifModifiedSince := context.GetHeader("If-Modified-Since")
		if ifModifiedSince != "" {
			t, err := time.Parse(http.TimeFormat, ifModifiedSince)
			if err == nil && !image.UpdatedAt.After(t) {
				context.Header("ETag", etag)
				context.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
				context.Status(http.StatusNotModified)
				context.Abort()
				return
			}
		}
	}

	context.Header("Content-Type", image.MimeType)
	context.Header("Cache-Control", "public, max-age=2592000, immutable")
	context.Header("X-Content-Type-Options", "nosniff")
	context.Header("ETag", etag)
	context.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))
}

func handleMetadataError(context *gin.Context, identifier string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.RespondError(context, http.StatusNotFound, "Image not found")
	} else if errors.Is(err, ErrTemporaryFailure) {
		log.Printf("Transient error for identifier '%s'. Client should retry.", identifier)
		common.RespondError(context, http.StatusServiceUnavailable, "Service is temporarily busy, please try again.")
	} else {
		log.Printf("Database error when fetching identifier '%s': %v", identifier, err)
		common.RespondError(context, http.StatusInternalServerError, "Error retrieving image information")
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

// isBrokenPipe 跳过BrokenPipe错误
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
