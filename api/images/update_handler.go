package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/images"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils/validator"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UploadImageHandler handles the image upload process efficiently by reading the file only once.
func UploadImageHandler(context *gin.Context) {
	fileHeader, err := context.FormFile("file")
	cfg := config.Get()
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Image file is required")
		return
	}

	// open file
	file, err := fileHeader.Open()
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to open uploaded file")
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to read file content")
		return
	}

	isImage, mimeType := validator.IsImageBytes(fileBytes)
	if !isImage {
		common.RespondError(context, http.StatusUnsupportedMediaType, "The uploaded file type is not supported")
		return
	}

	hashBytes := sha256.Sum256(fileBytes)
	fileHash := hex.EncodeToString(hashBytes[:])

	image, err := images.GetImageByHash(fileHash)
	if err == nil {
		log.Printf("Duplicate image detected. Hash: %s, Identifier: %s", fileHash, image.Identifier)
		fullURL := buildImageURL(image.Identifier)
		common.RespondSuccessMessage(context, "Image already exists", gin.H{
			"identifier":    image.Identifier,
			"original_name": image.OriginalName,
			"file_size":     image.FileSize,
			"url":           fullURL,
		})
		return
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Database error when checking for hash '%s': %v", fileHash, err)
		common.RespondError(context, http.StatusInternalServerError, "Database error during hash check")
		return
	}

	// new image
	ext := filepath.Ext(fileHeader.Filename)
	identifier := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), fileHash[:16], ext)

	fileReader := bytes.NewReader(fileBytes)
	if err := storage.AppStorage.Save(identifier, fileReader); err != nil {
		log.Printf("Failed to save file to storage: %v", err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to save uploaded file")
		return
	}

	// 入库
	newImage := models.Image{
		Identifier:    identifier,
		OriginalName:  fileHeader.Filename,
		FileSize:      fileHeader.Size,
		MimeType:      mimeType,
		StorageDriver: cfg.Server.StorageConfig.Type,
		FileHash:      fileHash,
		UserID:        context.GetUint(middleware.ContextUserIDKey),
	}

	if err := images.SaveImage(&newImage); err != nil {
		log.Printf("Failed to create image record in database: %v", err)

		log.Printf("Attempting to delete orphaned file from storage: %s", identifier)
		if delErr := storage.AppStorage.Delete(identifier); delErr != nil {
			log.Printf("CRITICAL: Failed to delete orphaned file '%s' after db error. Manual cleanup may be required. Delete error: %v", identifier, delErr)
		}

		common.RespondError(context, http.StatusInternalServerError, "Failed to save image metadata")
		return
	}

	fullURL := buildImageURL(newImage.Identifier)
	common.RespondSuccessMessage(context, "Image uploaded successfully", gin.H{
		"identifier":    newImage.Identifier,
		"original_name": newImage.OriginalName,
		"file_size":     newImage.FileSize,
		"url":           fullURL,
	})
}

// buildImageURL Base URL for images
func buildImageURL(identifier string) string {
	cfg := config.Get()
	return fmt.Sprintf("%s/images/%s", cfg.Server.BaseURL, identifier)
}
