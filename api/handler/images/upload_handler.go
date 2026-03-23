package images

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
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

	if len(files) > 10 {
		common.RespondError(c, http.StatusBadRequest, "Maximum 10 files allowed per upload")
		return
	}

	// 获取配置
	ctx := c.Request.Context()
	settings, err := h.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get processing settings")
		return
	}

	// 检查文件大小限制
	if settings.MaxFileSizeMB > 0 {
		maxSize := int64(settings.MaxFileSizeMB) * 1024 * 1024
		for _, f := range files {
			if f.Size > maxSize {
				common.RespondError(c, http.StatusRequestEntityTooLarge, fmt.Sprintf("File %s size (%.2f MB) exceeds maximum allowed (%d MB)", f.Filename, float64(f.Size)/1024/1024, settings.MaxFileSizeMB))
				return
			}
		}
	}

	// 检查批量总大小限制（多文件时）
	if len(files) > 1 {
		var totalSize int64
		for _, f := range files {
			totalSize += f.Size
		}
		maxBatchTotalMB := settings.MaxBatchTotalMB
		if maxBatchTotalMB == 0 {
			maxBatchTotalMB = 500
		}
		maxTotalSize := int64(maxBatchTotalMB) * 1024 * 1024
		if totalSize > maxTotalSize {
			common.RespondError(c, http.StatusRequestEntityTooLarge, fmt.Sprintf("Total size of all files (%.2f MB) exceeds maximum allowed (%d MB)", float64(totalSize)/1024/1024, maxBatchTotalMB))
			return
		}
	}

	storageConfigID, err := h.resolveStorageConfigID(c)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	// 确定可见性
	isPublic := settings.DefaultVisibility != "private"
	if visibility := c.PostForm("is_public"); visibility != "" {
		isPublic = visibility != "false"
	}

	// 单文件：保持旧格式兼容
	if len(files) == 1 {
		result, err := h.imageService.UploadSingle(ctx, userID, files[0], storageConfigID, isPublic, settings.DefaultAlbumID)
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
	results, err := h.imageService.UploadBatch(ctx, userID, files, storageConfigID, isPublic, settings.DefaultAlbumID, settings.ConcurrentUploadLimit)
	if err != nil {
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
		"total_files":   len(files),
		"success_count": len(successResults),
		"error_count":   len(errorResults),
		"success":       successResults,
		"errors":        errorResults,
	})
}

// resolveStorageConfigID 解析存储配置ID
func (h *Handler) resolveStorageConfigID(c *gin.Context) (uint, error) {
	strategyIDStr := c.PostForm("strategy_id")
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
		return 0, nil
	}
	return defaultID, nil
}
