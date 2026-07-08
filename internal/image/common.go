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
	imageCommonLog   = utils.ForModule("Image")
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

// submitBackgroundTask 提交后台任务到 worker pool，队列满时丢弃并记录警告。
// 返回值表示任务是否成功进入后台队列。
func submitBackgroundTask(task func()) bool {
	pool := worker.GetGlobalPool()
	if pool == nil {
		imageCommonLog.Infof("Worker pool not initialized, dropping background task")
		return false
	}
	if ok := pool.Submit(task); !ok {
		imageCommonLog.Warnf("Worker pool queue full, dropping background task")
		return false
	}
	return true
}

// SubmitBackgroundTask 提供给包外构造器复用统一的后台任务提交逻辑。
func SubmitBackgroundTask(task func()) {
	_ = submitBackgroundTask(task)
}

func getReadableStorageProviderByID(storageID uint) (storage.Provider, error) {
	if storageID == 0 {
		provider := storage.GetDefault()
		if provider == nil {
			return nil, storage.ErrNoDefaultStorage
		}
		return provider, nil
	}

	provider, err := storage.GetByID(storageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get readable storage provider by ID %d: %w", storageID, err)
	}
	return provider, nil
}

func getWritableStorageProviderByID(storageID uint) (storage.Provider, error) {
	if storageID == 0 {
		provider := storage.GetDefault()
		if provider == nil {
			return nil, storage.ErrNoDefaultStorage
		}
		return provider, nil
	}

	provider, err := storage.GetWritableByID(storageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writable storage provider by ID %d: %w", storageID, err)
	}
	return provider, nil
}

// IsStorageUnavailable 判断错误是否表示目标存储或默认存储当前不可用，
// 供上层据此返回 503 Service Unavailable (F7 降级语义)。
func IsStorageUnavailable(err error) bool {
	return errors.Is(err, storage.ErrNoDefaultStorage) || errors.Is(err, storage.ErrProviderNotFound)
}

// IsStorageDisabled 判断错误是否表示目标存储已加载但被禁用（写访问），供上层返回 409。
func IsStorageDisabled(err error) bool {
	return errors.Is(err, storage.ErrProviderDisabled)
}

// CheckStorageAvailable 校验指定存储 ID（0 表示默认存储）对应的 Provider 是否可写就绪。
func CheckStorageAvailable(storageID uint) error {
	_, err := getWritableStorageProviderByID(storageID)
	return err
}

// resolveWritableStorageForUpload 从单一 registry 快照解析上传写入目标（provider + 解析后 ID）。
// storageID==0 解析当前默认存储；返回的 ID 即将落库到 image.StorageConfigID，
// 保证实际写入目标与记录的 StorageConfigID 一致（消除 DB 默认与 registry 默认的漂移）。
func resolveWritableStorageForUpload(storageID uint) (storage.Provider, uint, error) {
	return storage.ResolveWritable(storageID)
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
