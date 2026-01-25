package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/validator"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// UploadImageHandler handles single image upload
func UploadImageHandler(context *gin.Context) {
	form, err := context.MultipartForm()
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid form data")
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		common.RespondError(context, http.StatusBadRequest, "At least one file is required under the 'file' key")
		return
	}

	if len(files) > 1 {
		common.RespondError(context, http.StatusBadRequest, "Only one file is allowed for single upload")
		return
	}

	fileHeader := files[0]

	storageName := context.PostForm("storage")
	if storageName == "" {
		storageName = context.Query("storage")
	}

	storageClient, err := storage.GetStorage(storageName)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}
	driverToSave := storageName
	if driverToSave == "" {
		driverToSave = config.Get().Server.StorageConfig.Type
	}

	userID := context.GetUint(middleware.ContextUserIDKey)
	image, _, err := processAndSaveImage(userID, fileHeader, storageClient, driverToSave)
	if err != nil {
		if !context.IsAborted() {
			common.RespondError(context, http.StatusInternalServerError, err.Error())
		}
		return
		common.RespondError(context, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondSuccess(context, gin.H{
		"identifier": image.Identifier,
		"filename":   image.OriginalName,
		"file_size":  image.FileSize,
		"url":        utils.BuildImageURL(image.Identifier),
	})
}

// UploadImagesHandler Multiple file upload interface
func UploadImagesHandler(context *gin.Context) {
	form, err := context.MultipartForm()
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid form data")
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		common.RespondError(context, http.StatusBadRequest, "At least one file is required under the 'files' key")
		return
	}

	if len(files) > 10 { // 限制最大上传文件数量
		common.RespondError(context, http.StatusBadRequest, "Maximum 10 files allowed per upload")
		return
	}

	// P1 修复：检查总文件大小限制（500MB）
	var totalSize int64 = 0
	for _, f := range files {
		totalSize += f.Size
	}
	const maxTotalSize int64 = 500 * 1024 * 1024 // 500MB
	if totalSize > maxTotalSize {
		common.RespondError(context, http.StatusRequestEntityTooLarge, fmt.Sprintf("Total size of all files (%.2f MB) exceeds maximum allowed (%.2f MB)", float64(totalSize)/1024/1024, float64(maxTotalSize)/1024/1024))
		return
	}

	storageName := context.PostForm("storage")
	if storageName == "" {
		storageName = context.Query("storage")
	}

	storageClient, err := storage.GetStorage(storageName)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}
	driverToSave := storageName
	if driverToSave == "" {
		driverToSave = config.Get().Server.StorageConfig.Type
	}

	type uploadResult struct {
		Identifier string
		FileName   string
		FileSize   int64
		URL        string
		Error      string
	}

	results := make([]uploadResult, len(files))
	var resultsMutex sync.Mutex
	userID := context.GetUint(middleware.ContextUserIDKey)

	g, ctx := errgroup.WithContext(context.Request.Context())

	// 异步上传
	for i, fileHeader := range files {
		i, fileHeader := i, fileHeader
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				result := uploadResult{FileName: fileHeader.Filename}
				image, _, err := processAndSaveImage(userID, fileHeader, storageClient, driverToSave)

				if err != nil {
					// P0 修复：如果客户端断开，不记录错误
					if !context.IsAborted() {
						result.Error = err.Error()
					}
				} else {
					result.Identifier = image.Identifier
					result.FileSize = image.FileSize
					result.URL = utils.BuildImageURL(image.Identifier)
				}

				resultsMutex.Lock()
				results[i] = result
				resultsMutex.Unlock()
				return nil
			}
		})
	}

	if err := g.Wait(); err != nil {
		log.Printf("Error during concurrent upload processing: %v", err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to process uploads due to a context error")
		return
	}

	var successResults []gin.H
	var errorResults []gin.H
	for _, result := range results {
		if result.Error != "" {
			errorResults = append(errorResults, gin.H{"filename": result.FileName, "error": result.Error})
		} else {
			successResults = append(successResults, gin.H{
				"identifier": result.Identifier,
				"filename":   result.FileName,
				"file_size":  result.FileSize,
				"url":        result.URL,
			})
		}
	}

	common.RespondSuccess(context, gin.H{
		"message":       "Upload completed",
		"total_files":   len(files),
		"success_count": len(successResults),
		"error_count":   len(errorResults),
		"success":       successResults,
		"errors":        errorResults,
	})
}

// processAndSaveImage save image
func processAndSaveImage(userID uint, fileHeader *multipart.FileHeader, storageClient storage.Storage, driverToSave string) (*models.Image, bool, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	var buf bytes.Buffer
	teeReader := io.TeeReader(file, hasher)

	if _, err = io.Copy(&buf, teeReader); err != nil {
		return nil, false, fmt.Errorf("failed to process file stream: %w", err)
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))

	image, err := images.GetImageByHash(fileHash)
	if err == nil {
		log.Printf("Duplicate active image detected. Hash: %s, Identifier: %s", fileHash, image.Identifier)

		go warmCache(image)
		return image, true, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Database error when checking for active hash '%s': %v", fileHash, err)
		return nil, false, errors.New("database error during hash check")
	}

	softDeletedImage, err := images.GetSoftDeletedImageByHash(fileHash)
	if err == nil {
		log.Printf("Found a soft-deleted image with the same hash. Restoring it. Identifier: %s", softDeletedImage.Identifier)

		updateData := map[string]interface{}{
			"deleted_at":     nil,
			"original_name":  fileHeader.Filename,
			"user_id":        userID,
			"storage_driver": driverToSave,
		}

		restoredImage, err := images.UpdateImageByIdentifier(softDeletedImage.Identifier, updateData)
		if err != nil {
			log.Printf("Failed to restore soft-deleted image '%s': %v", softDeletedImage.Identifier, err)
			return nil, false, errors.New("failed to restore existing image data")
		}

		log.Printf("Image record restored successfully for identifier: %s", restoredImage.Identifier)
		go warmCache(restoredImage)
		return restoredImage, true, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Database error when checking for soft-deleted hash '%s': %v", fileHash, err)
		return nil, false, errors.New("database error during hash check")
	}

	log.Printf("No existing image found for hash %s. Proceeding with new upload.", fileHash)

	fileBytes := buf.Bytes()
	isImage, mimeType := validator.IsImageBytes(fileBytes)
	if !isImage {
		return nil, false, errors.New("the uploaded file type is not supported")
	}

	// 生成唯一标识符
	ext := filepath.Ext(fileHeader.Filename)
	identifier := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), fileHash[:16], ext)

	if err := storageClient.Save(identifier, bytes.NewReader(fileBytes)); err != nil {
		log.Printf("Failed to save file to storage: %v", err)
		return nil, false, errors.New("failed to save uploaded file")
	}

	// 入库
	newImage := &models.Image{
		Identifier:    identifier,
		OriginalName:  fileHeader.Filename,
		FileSize:      fileHeader.Size,
		MimeType:      mimeType,
		StorageDriver: driverToSave,
		FileHash:      fileHash,
		UserID:        userID,
	}

	if err := images.SaveImage(newImage); err != nil {
		log.Printf("Failed to create image record in database: %v", err)
		log.Printf("Attempting to delete orphaned file from storage: %s", identifier)
		if delErr := storageClient.Delete(identifier); delErr != nil {
			log.Printf("CRITICAL: Failed to delete orphaned file '%s'. Manual cleanup may be required. Delete error: %v", identifier, delErr)
		}
		return nil, false, errors.New("failed to save image metadata")
	}

	go warmCache(newImage)
	return newImage, false, nil
}

// warmCache 更新缓存
func warmCache(image *models.Image) {
	if err := cache.CacheImage(image); err != nil {
		log.Printf("WARN: Failed to pre-warm cache for image '%s': %v", image.Identifier, err)
	}
}
