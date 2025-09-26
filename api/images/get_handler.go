package images

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/images"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var imageGroup singleflight.Group

// GetImageHandler serves an image file.
func GetImageHandler(context *gin.Context) {
	identifier := context.Param("identifier")
	if identifier == "" {
		common.RespondError(context, http.StatusBadRequest, "Image identifier is required")
		return
	}

	var image models.Image
	err := cache.GetCachedImage(identifier, &image)
	if err != nil {
		// 缓存未命中
		val, err, _ := imageGroup.Do(identifier, func() (interface{}, error) {
			imagePtr, err := images.GetImageByIdentifier(identifier)
			if err != nil {
				return nil, err
			}

			go func(img *models.Image) {
				if cacheErr := cache.CacheImage(img); cacheErr != nil {
					log.Printf("Failed to cache image metadata for '%s': %v", img.Identifier, cacheErr)
				}
			}(imagePtr)

			return imagePtr, nil
		})

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				common.RespondError(context, http.StatusNotFound, "Image not found")
			} else {
				log.Printf("Database error when fetching identifier '%s': %v", identifier, err)
				common.RespondError(context, http.StatusInternalServerError, "Error retrieving image information")
			}
			return
		}
		image = *(val.(*models.Image))
	}

	// 判断cache
	if imageData, err := cache.GetCachedImageData(identifier); err == nil {
		serveImageData(context, &image, imageData)
		return
	}

	// 缓存未命中
	storageClient, err := storage.GetStorage(image.StorageDriver)
	if err != nil {
		log.Printf("Failed to get storage client for driver '%s': %v", image.StorageDriver, err)
		common.RespondError(context, http.StatusInternalServerError, "Error retrieving storage client")
		return
	}

	imageStream, err := storageClient.Get(identifier)
	if err != nil {
		log.Printf("CRITICAL: File for identifier '%s' not found in storage, but exists in DB. Error: %v", identifier, err)
		common.RespondError(context, http.StatusNotFound, "Image file not found in storage")
		return
	}

	//关闭流
	defer func() {
		if closer, ok := imageStream.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	// 检查是否启用图片缓存
	cfg := config.Get()
	enableImageCaching := cfg.Server.CacheConfig.EnableImageCaching
	maxCacheSize := cfg.Server.CacheConfig.MaxImageCacheSize
	shouldCache := image.FileSize > 0 && (maxCacheSize == 0 || image.FileSize <= maxCacheSize)

	if shouldCache && enableImageCaching {
		imageData, err := io.ReadAll(imageStream)
		if err != nil {
			log.Printf("Failed to read image stream into buffer for '%s': %v", identifier, err)
			common.RespondError(context, http.StatusInternalServerError, "Failed to read image file")
			return
		}

		go func(id string, data []byte) {
			if cacheErr := cache.CacheImageData(id, data); cacheErr != nil {
				log.Printf("Failed to cache image data for '%s': %v", id, cacheErr)
			}
		}(identifier, imageData)

		serveImageData(context, &image, imageData)
	} else {
		serveImageStream(context, &image, imageStream)
	}
}

// serveImageData 提供图片服务
func serveImageData(context *gin.Context, image *models.Image, data []byte) {
	reader := bytes.NewReader(data)
	serveImageStream(context, image, reader)
}

// serveImageStream 提供图片服务
func serveImageStream(context *gin.Context, image *models.Image, stream io.ReadSeeker) {
	if _, ok := context.GetQuery("download"); ok {
		safeName := sanitizeFilename(image.OriginalName)
		contentDisposition := fmt.Sprintf(
			"attachment; filename=\"%s\"; filename*=UTF-8''%s", safeName, url.PathEscape(safeName),
		)
		context.Header("Content-Disposition", contentDisposition)
	}

	// ETag  If-Modified-Since
	etag := fmt.Sprintf("W/\"%x\"", image.UpdatedAt.UnixNano())
	if match := context.GetHeader("If-None-Match"); match == etag {
		context.Status(http.StatusNotModified)
		return
	}
	ifModifiedSince := context.GetHeader("If-Modified-Since")
	if ifModifiedSince != "" {
		t, err := time.Parse(http.TimeFormat, ifModifiedSince)
		if err == nil && !image.UpdatedAt.After(t) {
			context.Status(http.StatusNotModified)
			return
		}
	}

	// 设置响应头
	context.Header("Content-Type", image.MimeType)
	context.Header("Cache-Control", "public, max-age=2592000, immutable")
	context.Header("X-Content-Type-Options", "nosniff")
	context.Header("ETag", etag)
	context.Header("Last-Modified", image.UpdatedAt.UTC().Format(http.TimeFormat))

	http.ServeContent(context.Writer, context.Request, sanitizeFilename(image.OriginalName), image.UpdatedAt, stream)
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

func isBrokenPipe(err error) bool {
	return err != nil && (errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET))
}
