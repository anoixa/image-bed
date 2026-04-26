package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"sync"
	"time"

	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/anoixa/image-bed/utils/validator"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

var writeServiceLog = utils.ForModule("WriteService")

// WriteService 负责图片上传与写入相关用例
type WriteService struct {
	repo          *images.Repository
	albumsRepo    *albums.Repository
	converter     *Converter
	cacheHelper   *cache.Helper
	baseURL       string
	pathGenerator *generator.PathGenerator
}

func NewWriteService(
	repo *images.Repository,
	albumsRepo *albums.Repository,
	converter *Converter,
	cacheHelper *cache.Helper,
	baseURL string,
) *WriteService {
	return &WriteService{
		repo:          repo,
		albumsRepo:    albumsRepo,
		converter:     converter,
		cacheHelper:   cacheHelper,
		baseURL:       baseURL,
		pathGenerator: generator.NewPathGenerator(),
	}
}

// UploadSingle 单文件上传
func (s *WriteService) UploadSingle(
	ctx context.Context,
	userID uint,
	fileHeader *multipart.FileHeader,
	storageID uint,
	isPublic bool,
	defaultAlbumID uint,
) (*UploadResult, error) {
	storageProvider, err := getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	image, isDup, err := s.processAndSaveImage(ctx, userID, uploadSourceFromFileHeader(fileHeader), storageProvider, storageID, isPublic, defaultAlbumID)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		Image:       image,
		IsDuplicate: isDup,
		Identifier:  image.Identifier,
		FileName:    image.OriginalName,
		FileSize:    image.FileSize,
		Links:       utils.BuildLinkFormats(s.baseURL, image.Identifier),
	}, nil
}

// UploadSingleSource 单文件上传（基于已准备好的上传源）
func (s *WriteService) UploadSingleSource(
	ctx context.Context,
	userID uint,
	source UploadSource,
	storageID uint,
	isPublic bool,
	defaultAlbumID uint,
) (*UploadResult, error) {
	storageProvider, err := getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	image, isDup, err := s.processAndSaveImage(ctx, userID, source, storageProvider, storageID, isPublic, defaultAlbumID)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		Image:       image,
		IsDuplicate: isDup,
		Identifier:  image.Identifier,
		FileName:    image.OriginalName,
		FileSize:    image.FileSize,
		Links:       utils.BuildLinkFormats(s.baseURL, image.Identifier),
	}, nil
}

// UploadBatch 批量上传
func (s *WriteService) UploadBatch(ctx context.Context, userID uint, files []*multipart.FileHeader, storageID uint, isPublic bool, defaultAlbumID uint, concurrentLimit int) ([]*UploadResult, error) {
	sources := make([]UploadSource, 0, len(files))
	for _, fileHeader := range files {
		sources = append(sources, uploadSourceFromFileHeader(fileHeader))
	}
	return s.UploadBatchSources(ctx, userID, sources, storageID, isPublic, defaultAlbumID, concurrentLimit)
}

// UploadBatchSources 批量上传（基于已准备好的上传源）
func (s *WriteService) UploadBatchSources(ctx context.Context, userID uint, files []UploadSource, storageID uint, isPublic bool, defaultAlbumID uint, concurrentLimit int) ([]*UploadResult, error) {
	storageProvider, err := getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	if concurrentLimit <= 0 {
		concurrentLimit = 3
	}

	results := make([]*UploadResult, len(files))
	var resultsMutex sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, concurrentLimit)

	for i, fileHeader := range files {
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				image, _, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageID, isPublic, defaultAlbumID)
				result := &UploadResult{FileName: fileHeader.FileName}

				if err != nil {
					result.Error = err.Error()
				} else {
					result.Image = image
					result.Identifier = image.Identifier
					result.FileSize = image.FileSize
					result.Links = utils.BuildLinkFormats(s.baseURL, image.Identifier)
				}

				resultsMutex.Lock()
				results[i] = result
				resultsMutex.Unlock()
				return nil
			}
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("batch upload failed: %w", err)
	}

	return results, nil
}

// processAndSaveImage 处理并保存图片
func (s *WriteService) processAndSaveImage(ctx context.Context, userID uint, source UploadSource, storageProvider storage.Provider, storageConfigID uint, isPublic bool, defaultAlbumID uint) (resultImg *models.Image, isDup bool, retErr error) {
	processStart := time.Now()
	defer func() {
		middleware.RecordUploadFileProcessed(time.Since(processStart))
	}()

	// Ensure temp file is cleaned up on any exit path (panic, error, dedup)
	// unless ownership is explicitly transferred to the converter.
	tempFileConsumed := false
	if source.tempFilePath != "" {
		defer func() {
			if !tempFileConsumed {
				source.CleanupRequestTempFile()
			}
		}()
	}

	src, err := source.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = src.Close() }()

	header := make([]byte, 512)
	n, err := io.ReadFull(src, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, false, fmt.Errorf("failed to read file header: %w", err)
	}
	header = header[:n]

	isImage, mimeType := validator.IsImageBytes(header)
	if !isImage {
		return nil, false, errors.New("the uploaded file type is not supported")
	}

	var fileHash string
	if source.PrecomputedHash != "" {
		fileHash = source.PrecomputedHash
	} else {
		hashStart := time.Now()
		hash := sha256.New()
		if _, err := hash.Write(header); err != nil {
			return nil, false, fmt.Errorf("failed to hash file header: %w", err)
		}

		bufPtr := pool.SharedBufferPool.Get().(*[]byte)
		defer pool.SharedBufferPool.Put(bufPtr)

		if _, err = io.CopyBuffer(hash, src, *bufPtr); err != nil {
			return nil, false, fmt.Errorf("failed to hash file stream: %w", err)
		}

		fileHash = hex.EncodeToString(hash.Sum(nil))
		middleware.RecordUploadHashDuration(time.Since(hashStart))
	}

	img, err := s.repo.WithContext(ctx).GetImageByHash(fileHash)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	if err == nil {
		if img.UserID != userID {
			newImg, err := s.createDedupedImageRecord(ctx, img, userID, source.FileName, storageConfigID, isPublic)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create deduped image record: %w", err)
			}
			submitBackgroundTask(func() { s.warmCache(newImg) })
			return newImg, true, nil
		}

		submitBackgroundTask(func() { s.warmCache(img) })
		if s.converter != nil {
			middleware.RecordUploadTaskSubmit(submitBackgroundTask(func() { s.converter.TriggerConversion(img) }))
		}
		return img, true, nil
	}

	deletedImg, deletedErr := s.repo.WithContext(ctx).GetSoftDeletedImageByHash(fileHash)
	if deletedErr != nil && !errors.Is(deletedErr, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}
	if deletedErr == nil {
		reusable, err := s.canReuseSoftDeletedImage(ctx, deletedImg)
		if err != nil {
			writeServiceLog.Warnf("Failed to verify soft-deleted image %s for hash reuse: %v", utils.SanitizeLogMessage(deletedImg.Identifier), err)
		} else if reusable {
			if deletedImg.UserID != userID {
				newImg, err := s.createDedupedImageRecord(ctx, deletedImg, userID, source.FileName, storageConfigID, isPublic)
				if err != nil {
					return nil, false, fmt.Errorf("failed to create deduped image record: %w", err)
				}
				submitBackgroundTask(func() { s.warmCache(newImg) })
				return newImg, true, nil
			}

			updates := map[string]any{
				"deleted_at":    nil,
				"original_name": source.FileName,
				"is_public":     isPublic,
			}
			restored, err := s.repo.WithContext(ctx).UpdateImageByIdentifier(deletedImg.Identifier, updates)
			if err != nil {
				return nil, false, errors.New("failed to restore existing image data")
			}

			submitBackgroundTask(func() { s.warmCache(restored) })
			if s.converter != nil {
				middleware.RecordUploadTaskSubmit(submitBackgroundTask(func() { s.converter.TriggerConversion(restored) }))
			}

			return restored, true, nil
		}
	}

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek upload source: %w", err)
	}

	width, height := utils.GetImageDimensions(src)

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek upload source after dimension extraction: %w", err)
	}

	ext := getSafeFileExtension(mimeType)
	ids := s.pathGenerator.GenerateOriginalIdentifiers(fileHash, ext, time.Now())
	identifier := ids.Identifier
	storagePath := ids.StoragePath
	storageWriteStart := time.Now()
	if err := storageProvider.SaveWithContext(ctx, storagePath, src); err != nil {
		return nil, false, errors.New("failed to save uploaded file")
	}
	middleware.RecordUploadStorageWriteDuration(time.Since(storageWriteStart))

	actualFileSize, err := getUploadSourceSize(src, source.FileSize)
	if err != nil {
		return nil, false, fmt.Errorf("failed to determine file size: %w", err)
	}

	newImg := &models.Image{
		Identifier:      identifier,
		StoragePath:     storagePath,
		OriginalName:    source.FileName,
		FileSize:        actualFileSize,
		MimeType:        mimeType,
		StorageConfigID: storageConfigID,
		FileHash:        fileHash,
		Width:           width,
		Height:          height,
		IsPublic:        isPublic,
		UserID:          userID,
	}

	dbWriteStart := time.Now()
	if err := s.repo.WithContext(ctx).SaveImage(newImg); err != nil {
		_ = storageProvider.DeleteWithContext(ctx, storagePath)
		return nil, false, errors.New("failed to save image metadata")
	}
	middleware.RecordUploadDBWriteDuration(time.Since(dbWriteStart))

	if defaultAlbumID > 0 && s.albumsRepo != nil {
		if err := s.albumsRepo.AddImageToAlbum(defaultAlbumID, userID, newImg); err != nil {
			writeServiceLog.Warnf("Failed to add image to default album %d: %v", defaultAlbumID, err)
		}
	}

	submitBackgroundTask(func() { s.warmCache(newImg) })
	if s.converter != nil {
		if source.tempFilePath != "" {
			localFile := source.TransferTempFile()
			accepted := false
			if localFile != nil {
				accepted = submitBackgroundTask(func() { s.converter.TriggerConversionWithLocalFile(newImg, localFile) })
			}
			middleware.RecordUploadTaskSubmit(accepted)
			if accepted {
				tempFileConsumed = true
			} else if localFile != nil {
				localFile.CleanupTransferred()
			}
		} else {
			middleware.RecordUploadTaskSubmit(submitBackgroundTask(func() { s.converter.TriggerConversion(newImg) }))
		}
	}
	// converter == nil: tempFileConsumed stays false, defer cleans up

	return newImg, false, nil
}

func (s *WriteService) canReuseSoftDeletedImage(ctx context.Context, img *models.Image) (bool, error) {
	if img == nil || img.StoragePath == "" {
		return false, nil
	}

	refCount, err := s.repo.WithContext(ctx).CountImagesByStoragePath(img.StoragePath)
	if err != nil {
		return false, err
	}
	if refCount > 0 {
		return true, nil
	}

	provider, err := getStorageProviderByID(img.StorageConfigID)
	if err != nil {
		return false, err
	}

	exists, err := provider.Exists(ctx, img.StoragePath)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func getUploadSourceSize(src io.Seeker, hintedSize int64) (int64, error) {
	if hintedSize > 0 {
		return hintedSize, nil
	}

	currentPos, err := src.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	endPos, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := src.Seek(currentPos, io.SeekStart); err != nil {
		return 0, err
	}
	return endPos, nil
}

// createDedupedImageRecord 为不同用户创建去重后的新图片记录
func (s *WriteService) createDedupedImageRecord(ctx context.Context, existing *models.Image, userID uint, originalName string, _ uint, isPublic bool) (*models.Image, error) {
	ids := s.pathGenerator.GenerateOriginalIdentifiers(existing.FileHash+fmt.Sprintf("_%d", userID), filepath.Ext(originalName), time.Now())

	newImg := &models.Image{
		Identifier:      ids.Identifier,
		StoragePath:     existing.StoragePath,
		OriginalName:    originalName,
		FileSize:        existing.FileSize,
		MimeType:        existing.MimeType,
		StorageConfigID: existing.StorageConfigID,
		FileHash:        existing.FileHash,
		Width:           existing.Width,
		Height:          existing.Height,
		IsPublic:        isPublic,
		UserID:          userID,
	}

	if err := s.repo.WithContext(ctx).SaveImage(newImg); err != nil {
		return nil, err
	}

	return newImg, nil
}

func (s *WriteService) warmCache(image *models.Image) {
	if s.cacheHelper == nil {
		return
	}
	ctx, cancel := utils.DetachedContext(5 * time.Second)
	defer cancel()
	_ = s.cacheHelper.CacheImage(ctx, image)
}
