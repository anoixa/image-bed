package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"golang.org/x/sync/singleflight"
)

var (
	imageGroup       singleflight.Group
	metaFetchTimeout = 30 * time.Second
)

var (
	ErrTemporaryFailure = errors.New("temporary failure, should be retried")
	ErrForbidden        = errors.New("forbidden: access denied")
)

// ImageResultDTO DTO
type ImageResultDTO struct {
	Image      *models.Image
	Variant    *models.ImageVariant
	IsOriginal bool
	URL        string
	MIMEType   string
}

// UploadResult 上传结果
type UploadResult struct {
	Image       *models.Image
	IsDuplicate bool
	Identifier  string
	FileName    string
	FileSize    int64
	Links       utils.LinkFormats
	Error       string
}

// ImageResult 图片查询结果
type ImageResult struct {
	Image    *models.Image
	IsPublic bool
}

// ListImagesResult 图片列表结果
type ListImagesResult struct {
	Images     []*models.Image
	Total      int64
	Page       int
	Limit      int
	TotalPages int
}

// DeleteResult 删除结果
type DeleteResult struct {
	Success      bool
	DeletedCount int64
	Error        error
}

// submitBackgroundTask 提交后台任务到 worker pool，队列满时丢弃并记录警告
func submitBackgroundTask(task func()) {
	pool := worker.GetGlobalPool()
	if pool == nil {
		utils.Infof("[Image] Worker pool not initialized, dropping background task")
		return
	}
	if ok := pool.Submit(task); !ok {
		utils.Warnf("[Image] Worker pool queue full, dropping background task")
	}
}

// SubmitBackgroundTask 提供给包外构造器复用统一的后台任务提交逻辑。
func SubmitBackgroundTask(task func()) {
	submitBackgroundTask(task)
}

func getStorageProviderByID(storageID uint) (storage.Provider, error) {
	if storageID == 0 {
		provider := storage.GetDefault()
		if provider == nil {
			return nil, errors.New("no default storage configured")
		}
		return provider, nil
	}

	provider, err := storage.GetByID(storageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider by ID %d: %w", storageID, err)
	}
	return provider, nil
}

// getSafeFileExtension 根据MIME类型获取安全的文件扩展名
func getSafeFileExtension(mimeType string) string {
	ext := utils.GetSafeExtension(mimeType)
	if ext == "" {
		return ".bin"
	}
	return ext
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	var netErr interface {
		Timeout() bool
	}
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	return false
}
