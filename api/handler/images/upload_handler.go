package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/anoixa/image-bed/utils/validator"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// getSafeFileExtension 根据MIME类型获取安全的文件扩展名
func getSafeFileExtension(mimeType string) string {
	ext := utils.GetSafeExtension(mimeType)
	if ext == "" {
		// 默认使用 .bin 表示未知类型
		return ".bin"
	}
	return ext
}

// UploadImage 处理单图片上传
func (h *Handler) UploadImage(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid form data")
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		files = form.File["files"]
	}
	if len(files) == 0 {
		common.RespondError(c, http.StatusBadRequest, "At least one file is required under the 'file' or 'files' key")
		return
	}

	if len(files) > 1 {
		common.RespondError(c, http.StatusBadRequest, "Only one file is allowed for single upload")
		return
	}

	fileHeader := files[0]

	storageName := c.PostForm("storage")
	if storageName == "" {
		storageName = c.Query("storage")
	}

	storageProvider, err := h.storageFactory.Get(storageName)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	storageConfigID, err := h.getStorageConfigID(c, storageName)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	// 读取公开/私有参数，默认为公开
	isPublic := c.PostForm("is_public") != "false"
	image, _, err := h.processAndSaveImage(c.Request.Context(), userID, fileHeader, storageProvider, storageConfigID, isPublic)
	if err != nil {
		if !c.IsAborted() {
			common.RespondError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	common.RespondSuccess(c, gin.H{
		"identifier": image.Identifier,
		"filename":   image.OriginalName,
		"file_size":  image.FileSize,
		"links":      utils.BuildLinkFormats(image.Identifier),
	})
}

// UploadImages 处理多图片上传
func (h *Handler) UploadImages(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid form data")
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		common.RespondError(c, http.StatusBadRequest, "At least one file is required under the 'files' key")
		return
	}

	// 限制最大上传文件数量
	if len(files) > 10 {
		common.RespondError(c, http.StatusBadRequest, "Maximum 10 files allowed per upload")
		return
	}

	// 检查总文件大小限制
	var totalSize int64 = 0
	for _, f := range files {
		totalSize += f.Size
	}
	maxBatchTotalMB := config.Get().Server.Upload.MaxBatchTotalMB
	maxTotalSize := int64(maxBatchTotalMB) * 1024 * 1024
	if totalSize > maxTotalSize {
		common.RespondError(c, http.StatusRequestEntityTooLarge, fmt.Sprintf("Total size of all files (%.2f MB) exceeds maximum allowed (%d MB)", float64(totalSize)/1024/1024, maxBatchTotalMB))
		return
	}

	// 使用 storage_id 选择存储配置
	var storageProvider storage.Provider
	var storageConfigID uint

	storageIDStr := c.PostForm("storage_id")
	if storageIDStr == "" {
		storageIDStr = c.Query("storage_id")
	}

	if storageIDStr != "" {
		// 按 ID 获取存储
		id, parseErr := strconv.ParseUint(storageIDStr, 10, 32)
		if parseErr != nil {
			common.RespondError(c, http.StatusBadRequest, "Invalid storage_id")
			return
		}
		storageConfigID = uint(id)
		storageProvider, err = h.storageFactory.GetByID(storageConfigID)
		if err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		// 使用默认存储
		storageProvider = h.storageFactory.GetDefault()
		if storageProvider == nil {
			common.RespondError(c, http.StatusInternalServerError, "No default storage configured")
			return
		}
		storageConfigID = h.storageFactory.GetDefaultID()
	}

	type uploadResult struct {
		Identifier string
		FileName   string
		FileSize   int64
		Links      utils.LinkFormats
		Error      string
	}

	results := make([]uploadResult, len(files))
	var resultsMutex sync.Mutex
	userID := c.GetUint(middleware.ContextUserIDKey)
	// 读取公开/私有参数，默认为公开
	isPublic := c.PostForm("is_public") != "false"

	g, ctx := errgroup.WithContext(c.Request.Context())

	// 异步上传
	for i, fileHeader := range files {
		i, fileHeader := i, fileHeader
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				result := uploadResult{FileName: fileHeader.Filename}
				image, _, err := h.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageConfigID, isPublic)

				if err != nil {
					// 如果客户端断开，不记录错误
					if !c.IsAborted() {
						result.Error = err.Error()
					}
				} else {
					result.Identifier = image.Identifier
					result.FileSize = image.FileSize
					result.Links = utils.BuildLinkFormats(image.Identifier)
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
		common.RespondError(c, http.StatusInternalServerError, "Failed to process uploads due to a context error")
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
				"links":      result.Links,
			})
		}
	}

	common.RespondSuccess(c, gin.H{
		"message":       "Upload completed",
		"total_files":   len(files),
		"success_count": len(successResults),
		"error_count":   len(errorResults),
		"success":       successResults,
		"errors":        errorResults,
	})
}

// processAndSaveImage 保存图片
func (h *Handler) processAndSaveImage(ctx context.Context, userID uint, fileHeader *multipart.FileHeader, storageProvider storage.Provider, storageConfigID uint, isPublic bool) (*models.Image, bool, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 不再使用内存
	tempFile, err := os.CreateTemp("./data/temp", "upload-*")
	if err != nil {
		return nil, false, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name()) // 清理临时文件
	}()

	// 流式计算哈希并写入临时文件
	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)

	// 使用共享缓冲池优化复制
	buf := pool.SharedBufferPool.Get().([]byte)
	defer pool.SharedBufferPool.Put(buf)

	if _, err = io.CopyBuffer(writer, file, buf); err != nil {
		return nil, false, fmt.Errorf("failed to process file stream: %w", err)
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))

	// 检查重复文件
	image, err := h.repo.GetImageByHash(fileHash)
	if err == nil {
		go h.warmCache(image)

		// 触发 WebP 转换
		go h.converter.TriggerWebPConversion(image)
		// 触发缩略图生成
		go h.thumbnailService.TriggerGenerationForAllSizes(image)
		return image, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 检查软删除的文件
	softDeletedImage, err := h.repo.GetSoftDeletedImageByHash(fileHash)
	if err == nil {
		updateData := map[string]interface{}{
			"deleted_at":        nil,
			"original_name":     fileHeader.Filename,
			"user_id":           userID,
			"storage_config_id": storageConfigID,
		}

		restoredImage, err := h.repo.UpdateImageByIdentifier(softDeletedImage.Identifier, updateData)
		if err != nil {
			return nil, false, errors.New("failed to restore existing image data")
		}

		go h.warmCache(restoredImage)

		// 触发 WebP 转换
		go h.converter.TriggerWebPConversion(restoredImage)
		// 触发缩略图生成
		go h.thumbnailService.TriggerGenerationForAllSizes(restoredImage)
		return restoredImage, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 验证文件类型（读取前512字节）
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}

	fileHeaderBuf := make([]byte, 512)
	n, _ := tempFile.Read(fileHeaderBuf)
	fileHeaderBuf = fileHeaderBuf[:n]

	isImage, mimeType := validator.IsImageBytes(fileHeaderBuf)
	if !isImage {
		return nil, false, errors.New("the uploaded file type is not supported")
	}

	// 生成唯一标识符
	identifier := fileHash[:12]

	// 保存到存储
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}

	if err := storageProvider.SaveWithContext(ctx, identifier, tempFile); err != nil {
		return nil, false, errors.New("failed to save uploaded file")
	}

	// 创建数据库记录
	newImage := &models.Image{
		Identifier:      identifier,
		OriginalName:    fileHeader.Filename,
		FileSize:        fileHeader.Size,
		MimeType:        mimeType,
		StorageConfigID: storageConfigID,
		FileHash:        fileHash,
		IsPublic:        isPublic,
		UserID:          userID,
	}

	if err := h.repo.SaveImage(newImage); err != nil {
		storageProvider.DeleteWithContext(ctx, identifier)
		return nil, false, errors.New("failed to save image metadata")
	}

	go h.warmCache(newImage)

	// 触发 WebP 转换
	go h.converter.TriggerWebPConversion(newImage)
	// 触发缩略图生成
	go h.thumbnailService.TriggerGenerationForAllSizes(newImage)

	return newImage, false, nil
}

// warmCache 更新缓存
func (h *Handler) warmCache(image *models.Image) {
	if h.cacheHelper == nil {
		return
	}
	ctx := context.Background()
	if err := h.cacheHelper.CacheImage(ctx, image); err != nil {
		log.Printf("WARN: Failed to pre-warm cache for image '%s': %v", utils.SanitizeLogMessage(image.Identifier), err)
	}
}
