package images

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/images"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var imageGroup singleflight.Group

var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024) // 32KB
	},
}

// GetImageHandler serves an image file by first retrieving its metadata from the database
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
			// 回写缓存
			if cacheErr := cache.CacheImage(imagePtr); cacheErr != nil {
				log.Printf("Failed to cache image metadata: %v", cacheErr)
			}
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

	storageClient, err := storage.GetStorage(image.StorageDriver)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Error retrieving image information")
		return
	}

	imageStream, err := storageClient.Get(identifier)
	if err != nil {
		log.Printf("CRITICAL: File for identifier '%s' not found in storage, but exists in DB. Error: %v", identifier, err)
		common.RespondError(context, http.StatusNotFound, "Image file not found in storage")
		return
	}

	// 关闭流
	if closer, ok := imageStream.(io.Closer); ok {
		defer closer.Close()
	}

	// 下载模式
	if _, ok := context.GetQuery("download"); ok {
		safeName := sanitizeFilename(image.OriginalName)
		contentDisposition := fmt.Sprintf(
			"attachment; filename=\"%s\"; filename*=UTF-8''%s", safeName, url.PathEscape(safeName),
		)
		context.Header("Content-Disposition", contentDisposition)
	}

	// 设置响应头
	context.Header("Content-Type", image.MimeType)
	context.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
	context.Header("Cache-Control", "public, max-age=2592000, immutable")

	// ETag
	etag := fmt.Sprintf(`"%d-%s"`, image.FileSize, identifier)
	context.Header("ETag", etag)
	if match := context.GetHeader("If-None-Match"); match == etag {
		context.Status(http.StatusNotModified)
		return
	}

	seeker, ok := imageStream.(io.ReadSeeker)
	if ok {
		context.Header("Accept-Ranges", "bytes")
		http.ServeContent(context.Writer, context.Request, image.OriginalName, time.Now(), seeker)
		return
	}

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	_, err = io.CopyBuffer(context.Writer, imageStream, buf)
	if err != nil && !isBrokenPipe(err) {
		log.Printf("Failed to stream image to client: %v", err)
	}
}

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
