package images

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/gin-gonic/gin"
)

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

	// 获取存储配置：优先使用 strategy_id，否则使用默认存储
	storageConfigID, err := h.resolveStorageConfigID(c)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	isPublic := c.PostForm("is_public") != "false"

	result, err := h.imageService.UploadSingle(c.Request.Context(), userID, fileHeader, storageConfigID, isPublic)
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
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	maxBatchTotalMB := config.Get().UploadMaxBatchTotalMB
	maxTotalSize := int64(maxBatchTotalMB) * 1024 * 1024
	if totalSize > maxTotalSize {
		common.RespondError(c, http.StatusRequestEntityTooLarge, fmt.Sprintf("Total size of all files (%.2f MB) exceeds maximum allowed (%d MB)", float64(totalSize)/1024/1024, maxBatchTotalMB))
		return
	}

	// 获取存储配置：优先使用 strategy_id，否则使用默认存储
	storageConfigID, err := h.resolveStorageConfigID(c)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	isPublic := c.PostForm("is_public") != "false"

	results, err := h.imageService.UploadBatch(c.Request.Context(), userID, files, storageConfigID, isPublic)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to process uploads")
		return
	}

	// 转换结果
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
func (h *Handler) resolveStorageConfigID(c interface {
	PostForm(string) string
	Query(string) string
}) (uint, error) {
	// 优先从 form 中获取，如果不存在则从 query 中获取
	strategyIDStr := c.PostForm("strategy_id")
	if strategyIDStr == "" {
		strategyIDStr = c.Query("strategy_id")
	}

	// 获取对应的存储配置ID
	if strategyIDStr != "" {
		strategyID, err := strconv.ParseUint(strategyIDStr, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid strategy_id: %s", strategyIDStr)
		}
		return uint(strategyID), nil
	}

	// 未指定 strategy_id，返回 0 表示使用默认存储
	return 0, nil
}

