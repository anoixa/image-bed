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
	"time"
	"unicode"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

var (
	fileDownloadGroup singleflight.Group
)


// GetImage 获取图片（支持格式协商）
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
		// 构建 VariantResult 供 serveVariantImage 使用
		variantResult := &image.VariantResult{
			IsOriginal: false,
			Variant:    result.Variant,
			MIMEType:   result.MIMEType,
			Identifier: result.Variant.Identifier,
		}
		h.serveVariantImage(c, result.Image, variantResult)
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
		utils.SafeGo(func() {
			if h.cacheHelper != nil {
				_ = h.cacheHelper.CacheImageData(context.Background(), identifier, data)
			}
		})

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
	utils.SafeGo(func() {
		h.warmCache(image)
	})

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

// handleMetadataError 处理元数据查询错误
func (h *Handler) handleMetadataError(c *gin.Context, identifier string, err error) {
	if errors.Is(err, image.ErrForbidden) {
		common.RespondError(c, http.StatusForbidden, "This image is private")
		return
	}

	log.Printf("Failed to fetch image metadata for '%s': %v", identifier, err)
	common.RespondError(c, http.StatusNotFound, "Image not found")
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

