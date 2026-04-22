package images

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	imagesvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/gin-gonic/gin"
)

// UploadImage 统一图片上传接口（支持单文件和多文件）
// @Summary      Upload images
// @Description  Upload one or multiple image files (max 10 per request)
// @Tags         images
// @Accept       multipart/form-data
// @Produce      json
// @Param        files        formData  file    true   "Image file(s) to upload (max 10)"
// @Param        strategy_id  formData  string  false  "Storage strategy ID"
// @Param        is_public    formData  bool    false  "Whether images are public (default: true)"
// @Success      200  {object}  common.Response  "Upload successful"
// @Failure      400  {object}  common.Response  "Invalid form data or too many files"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      413  {object}  common.Response  "File too large"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/images/upload [post]
func (h *Handler) UploadImage(c *gin.Context) {
	if c.IsAborted() {
		return
	}

	// 获取配置
	ctx := c.Request.Context()
	settings, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		imageHandlerLog.Errorf("Failed to get processing settings: %v", err)
		common.RespondError(c, http.StatusInternalServerError, "Failed to get processing settings")
		return
	}

	request, cleanup, err := parseMultipartUploadRequest(c.Request, settings)
	if err != nil {
		if uploadErr, ok := err.(*uploadRequestError); ok {
			common.RespondError(c, uploadErr.status, uploadErr.message)
			return
		}
		imageHandlerLog.Errorf("Failed to parse multipart request: %v", err)
		common.RespondError(c, http.StatusBadRequest, "Invalid form data")
		return
	}
	defer cleanup()

	if len(request.files) == 0 {
		common.RespondError(c, http.StatusBadRequest, "At least one file is required under the 'files' key")
		return
	}

	storageConfigID, err := h.resolveStorageConfigIDValue(c, request.strategyID)
	if err != nil {
		imageHandlerLog.Errorf("Failed to resolve storage config: %v", err)
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	// 确定可见性
	isPublic := settings.DefaultVisibility != "private"
	if request.visibility != "" {
		isPublic = request.visibility != "false"
	}

	// 单文件：保持旧格式兼容
	if len(request.files) == 1 {
		result, err := h.writeService.UploadSingleSource(ctx, userID, request.files[0], storageConfigID, isPublic, settings.DefaultAlbumID)
		if err != nil {
			if !c.IsAborted() {
				common.RespondError(c, http.StatusInternalServerError, err.Error())
			}
			return
		}

		common.RespondSuccess(c, gin.H{
			"identifier": result.Identifier,
			"filename":   result.FileName,
			"file_size":  result.FileSize,
			"links":      result.Links,
		})
		return
	}

	// 多文件：返回批量格式
	results, err := h.writeService.UploadBatchSources(ctx, userID, request.files, storageConfigID, isPublic, settings.DefaultAlbumID, settings.ConcurrentUploadLimit)
	if err != nil {
		imageHandlerLog.Errorf("Failed to process batch upload for user=%d: %v", userID, err)
		if !c.IsAborted() {
			common.RespondError(c, http.StatusInternalServerError, "Failed to process uploads")
		}
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
		"total_files":   len(request.files),
		"success_count": len(successResults),
		"error_count":   len(errorResults),
		"success":       successResults,
		"errors":        errorResults,
	})
}

func (h *Handler) resolveStorageConfigIDValue(c *gin.Context, strategyIDStr string) (uint, error) {
	if strategyIDStr == "" {
		strategyIDStr = c.Query("strategy_id")
	}

	if strategyIDStr != "" {
		strategyID, err := strconv.ParseUint(strategyIDStr, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid strategy_id: %s", strategyIDStr)
		}
		return uint(strategyID), nil
	}

	defaultID, err := h.configManager.GetDefaultStorageConfigID(c.Request.Context())
	if err != nil {
		return 0, err
	}
	return defaultID, nil
}

type parsedUploadRequest struct {
	files      []imagesvc.UploadSource
	strategyID string
	visibility string
}

type uploadRequestError struct {
	status  int
	message string
}

func (e *uploadRequestError) Error() string {
	return e.message
}

type uploadTempFile struct {
	path string
}

func (f uploadTempFile) cleanup() {
	_ = os.Remove(f.path)
}

func parseMultipartUploadRequest(r *http.Request, settings *dbconfig.ImageProcessingSettings) (*parsedUploadRequest, func(), error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, &uploadRequestError{status: http.StatusBadRequest, message: "Invalid form data"}
	}

	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}

	request := &parsedUploadRequest{}
	var (
		tempFiles []uploadTempFile
		totalSize int64
	)

	cleanup := func() {
		for _, tempFile := range tempFiles {
			tempFile.cleanup()
		}
		for _, f := range request.files {
			f.CleanupRequestTempFile()
		}
	}

	maxFileSize := int64(0)
	if settings.MaxFileSizeMB > 0 {
		maxFileSize = int64(settings.MaxFileSizeMB) * 1024 * 1024
	}

	maxBatchTotalMB := settings.MaxBatchTotalMB
	if maxBatchTotalMB == 0 {
		maxBatchTotalMB = 500
	}
	maxTotalSize := int64(maxBatchTotalMB) * 1024 * 1024

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return nil, nil, &uploadRequestError{status: http.StatusBadRequest, message: "Invalid form data"}
		}

		partName := part.FormName()
		fileName := part.FileName()

		if fileName == "" {
			value, readErr := readSmallFormField(part, 4096)
			_ = part.Close()
			if readErr != nil {
				cleanup()
				return nil, nil, &uploadRequestError{status: http.StatusBadRequest, message: "Invalid form data"}
			}
			switch partName {
			case "strategy_id":
				request.strategyID = value
			case "is_public":
				request.visibility = strings.ToLower(value)
			}
			continue
		}

		if partName != "files" {
			_ = part.Close()
			continue
		}

		if len(request.files) >= 10 {
			_ = part.Close()
			cleanup()
			return nil, nil, &uploadRequestError{status: http.StatusBadRequest, message: "Maximum 10 files allowed per upload"}
		}

		tempFile, size, fileHash, writeErr := writePartToTempFile(part, maxFileSize)
		_ = part.Close()
		if writeErr != nil {
			cleanup()
			var sizeErr *uploadRequestError
			if ok := asUploadRequestError(writeErr, &sizeErr); ok {
				return nil, nil, sizeErr
			}
			return nil, nil, writeErr
		}

		totalSize += size
		if totalSize > maxTotalSize {
			tempFile.cleanup()
			cleanup()
			return nil, nil, &uploadRequestError{
				status:  http.StatusRequestEntityTooLarge,
				message: fmt.Sprintf("Total size of all files (%.2f MB) exceeds maximum allowed (%d MB)", float64(totalSize)/1024/1024, maxBatchTotalMB),
			}
		}

		// Transfer temp file ownership to WriteService/Pipeline — do NOT add to tempFiles.
		src := imagesvc.NewTempUploadSource(fileName, tempFile.path, size)
		src.TempFilePath = tempFile.path
		src.PrecomputedHash = fileHash
		request.files = append(request.files, src)
	}

	return request, cleanup, nil
}

func readSmallFormField(part io.Reader, maxBytes int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(part, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("form field too large")
	}
	return string(data), nil
}

func writePartToTempFile(part io.Reader, maxFileSize int64) (uploadTempFile, int64, string, error) {
	tmp, err := os.CreateTemp(config.TempDir, "upload-stream-*")
	if err != nil {
		return uploadTempFile{}, 0, "", fmt.Errorf("create temp file: %w", err)
	}

	tempFile := uploadTempFile{path: tmp.Name()}
	cleanup := func() {
		_ = tmp.Close()
		tempFile.cleanup()
	}

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	reader := io.Reader(part)
	if maxFileSize > 0 {
		reader = io.LimitReader(part, maxFileSize+1)
	}

	hash := sha256.New()
	written, err := io.CopyBuffer(io.MultiWriter(tmp, hash), reader, *bufPtr)
	if err != nil {
		cleanup()
		return uploadTempFile{}, 0, "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		tempFile.cleanup()
		return uploadTempFile{}, 0, "", fmt.Errorf("close temp file: %w", err)
	}

	if maxFileSize > 0 && written > maxFileSize {
		tempFile.cleanup()
		return uploadTempFile{}, 0, "", &uploadRequestError{
			status:  http.StatusRequestEntityTooLarge,
			message: fmt.Sprintf("File size (%.2f MB) exceeds maximum allowed (%d MB)", float64(written)/1024/1024, maxFileSize/(1024*1024)),
		}
	}

	return tempFile, written, hex.EncodeToString(hash.Sum(nil)), nil
}

func asUploadRequestError(err error, target **uploadRequestError) bool {
	requestErr, ok := err.(*uploadRequestError)
	if !ok {
		return false
	}
	*target = requestErr
	return true
}
