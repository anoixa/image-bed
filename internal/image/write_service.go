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
func (s *WriteService) processAndSaveImage(ctx context.Context, userID uint, source UploadSource, storageProvider storage.Provider, storageConfigID uint, isPublic bool, defaultAlbumID uint) (*models.Image, bool, error) {
	src, err := source.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = src.Close() }()

	header := make([]byte, 512)
	n, _ := io.ReadFull(src, header)
	header = header[:n]

	isImage, mimeType := validator.IsImageBytes(header)
	if !isImage {
		return nil, false, errors.New("the uploaded file type is not supported")
	}

	hash := sha256.New()
	if _, err := hash.Write(header); err != nil {
		return nil, false, fmt.Errorf("failed to hash file header: %w", err)
	}

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	if _, err = io.CopyBuffer(hash, src, *bufPtr); err != nil {
		return nil, false, fmt.Errorf("failed to hash file stream: %w", err)
	}

	fileHash := hex.EncodeToString(hash.Sum(nil))

	img, err := s.repo.GetImageByHash(fileHash)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	if err == nil && img.DeletedAt.Valid {
		if img.UserID != userID {
			newImg, err := s.createDedupedImageRecord(img, userID, source.FileName, storageConfigID, isPublic)
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
		restored, err := s.repo.UpdateImageByIdentifier(img.Identifier, updates)
		if err != nil {
			return nil, false, errors.New("failed to restore existing image data")
		}

		submitBackgroundTask(func() { s.warmCache(restored) })
		if s.converter != nil {
			submitBackgroundTask(func() { s.converter.TriggerConversion(restored) })
		}

		return restored, true, nil
	}

	if err == nil {
		if img.UserID != userID {
			newImg, err := s.createDedupedImageRecord(img, userID, source.FileName, storageConfigID, isPublic)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create deduped image record: %w", err)
			}
			submitBackgroundTask(func() { s.warmCache(newImg) })
			return newImg, true, nil
		}

		submitBackgroundTask(func() { s.warmCache(img) })
		if s.converter != nil {
			submitBackgroundTask(func() { s.converter.TriggerConversion(img) })
		}
		return img, true, nil
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
	if err := storageProvider.SaveWithContext(ctx, storagePath, src); err != nil {
		return nil, false, errors.New("failed to save uploaded file")
	}

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

	if err := s.repo.SaveImage(newImg); err != nil {
		_ = storageProvider.DeleteWithContext(ctx, storagePath)
		return nil, false, errors.New("failed to save image metadata")
	}

	if defaultAlbumID > 0 && s.albumsRepo != nil {
		if err := s.albumsRepo.AddImageToAlbum(defaultAlbumID, userID, newImg); err != nil {
			writeServiceLog.Warnf("Failed to add image to default album %d: %v", defaultAlbumID, err)
		}
	}

	submitBackgroundTask(func() { s.warmCache(newImg) })
	if s.converter != nil {
		submitBackgroundTask(func() { s.converter.TriggerConversion(newImg) })
	}

	return newImg, false, nil
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
func (s *WriteService) createDedupedImageRecord(existing *models.Image, userID uint, originalName string, _ uint, isPublic bool) (*models.Image, error) {
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

	if err := s.repo.SaveImage(newImg); err != nil {
		return nil, err
	}

	return newImg, nil
}

func (s *WriteService) warmCache(image *models.Image) {
	if s.cacheHelper == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.cacheHelper.CacheImage(ctx, image)
}
