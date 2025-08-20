package images

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/database/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetImageHandler serves an image file by first retrieving its metadata from the database,
func GetImageHandler(context *gin.Context) {
	identifier := context.Param("identifier")
	if identifier == "" {
		common.RespondError(context, http.StatusBadRequest, "Image identifier is required")
		return
	}

	// 查询元数据
	image, err := images.GetImageByIdentifier(identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(context, http.StatusNotFound, "Image not found")
		} else {
			log.Printf("Database error when fetching identifier '%s': %v", identifier, err)
			common.RespondError(context, http.StatusInternalServerError, "Error retrieving image information")
		}
		return
	}

	imageStream, err := storage.AppStorage.Get(identifier)
	if err != nil {
		log.Printf("CRITICAL: File for identifier '%s' not found in storage, but exists in DB. Error: %v", identifier, err)
		common.RespondError(context, http.StatusNotFound, "Image file not found in storage")
		return
	}
	defer imageStream.Close()

	if _, ok := context.GetQuery("download"); ok {
		// 触发浏览器下载
		context.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", image.OriginalName))
	}

	context.Header("Content-Type", image.MimeType)
	context.Header("Content-Length", strconv.FormatInt(image.FileSize, 10))
	context.Header("Cache-Control", "public, max-age=2592000") // 缓存30天

	_, err = io.Copy(context.Writer, imageStream)
	if err != nil {
		log.Printf("Failed to stream image to client: %v", err)
	}
}
