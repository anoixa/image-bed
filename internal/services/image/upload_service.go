package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"sync"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/anoixa/image-bed/utils/validator"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

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

// UploadService 图片上传服务
type UploadService struct {
	repo             images.RepositoryInterface
	variantRepo      images.VariantRepository
	storageFactory   *storage.Factory
	converter        *Converter
	thumbnailService *ThumbnailService
	cacheHelper      *cache.Helper
}

// NewUploadService 创建上传服务
func NewUploadService(
	repo images.RepositoryInterface,
	variantRepo images.VariantRepository,
	storageFactory *storage.Factory,
	converter *Converter,
	thumbnailService *ThumbnailService,
	cacheHelper *cache.Helper,
) *UploadService {
	return &UploadService{
		repo:             repo,
		variantRepo:      variantRepo,
		storageFactory:   storageFactory,
		converter:        converter,
		thumbnailService: thumbnailService,
		cacheHelper:      cacheHelper,
	}
}

// UploadSingle 单文件上传
func (s *UploadService) UploadSingle(
	ctx context.Context,
	userID uint,
	fileHeader *multipart.FileHeader,
	storageName string,
	isPublic bool,
) (*UploadResult, error) {
	storageProvider, storageConfigID, err := s.getStorageProvider(storageName)
	if err != nil {
		return nil, err
	}

	image, isDup, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageConfigID, isPublic)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		Image:       image,
		IsDuplicate: isDup,
		Identifier:  image.Identifier,
		FileName:    image.OriginalName,
		FileSize:    image.FileSize,
		Links:       utils.BuildLinkFormats(image.Identifier),
	}, nil
}

// UploadBatch 批量上传
func (s *UploadService) UploadBatch(
	ctx context.Context,
	userID uint,
	files []*multipart.FileHeader,
	storageName string,
	isPublic bool,
) ([]*UploadResult, error) {
	storageProvider, storageConfigID, err := s.getStorageProvider(storageName)
	if err != nil {
		return nil, err
	}

	results := make([]*UploadResult, len(files))
	var resultsMutex sync.Mutex

	g, ctx := errgroup.WithContext(ctx)

	for i, fileHeader := range files {
		i, fileHeader := i, fileHeader
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				image, _, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageConfigID, isPublic)
				result := &UploadResult{
					FileName: fileHeader.Filename,
				}

				if err != nil {
					result.Error = err.Error()
				} else {
					result.Image = image
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
		return nil, fmt.Errorf("batch upload failed: %w", err)
	}

	return results, nil
}

// processAndSaveImage 处理并保存图片
func (s *UploadService) processAndSaveImage(
	ctx context.Context,
	userID uint,
	fileHeader *multipart.FileHeader,
	storageProvider storage.Provider,
	storageConfigID uint,
	isPublic bool,
) (*models.Image, bool, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 创建临时文件
	tempFile, err := os.CreateTemp("./data/temp", "upload-*")
	if err != nil {
		return nil, false, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}()

	// 流式计算哈希
	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)

	buf := pool.SharedBufferPool.Get().([]byte)
	defer pool.SharedBufferPool.Put(buf)

	if _, err = io.CopyBuffer(writer, file, buf); err != nil {
		return nil, false, fmt.Errorf("failed to process file stream: %w", err)
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))

	// 检查重复文件
	image, err := s.repo.GetImageByHash(fileHash)
	if err == nil {
		go s.warmCache(image)
		go s.converter.TriggerWebPConversion(image)
		go s.thumbnailService.TriggerGenerationForAllSizes(image)
		return image, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 检查软删除的文件
	softDeletedImage, err := s.repo.GetSoftDeletedImageByHash(fileHash)
	if err == nil {
		updateData := map[string]interface{}{
			"deleted_at":        nil,
			"original_name":     fileHeader.Filename,
			"user_id":           userID,
			"storage_config_id": storageConfigID,
		}

		restoredImage, err := s.repo.UpdateImageByIdentifier(softDeletedImage.Identifier, updateData)
		if err != nil {
			return nil, false, errors.New("failed to restore existing image data")
		}

		go s.warmCache(restoredImage)
		go s.converter.TriggerWebPConversion(restoredImage)
		go s.thumbnailService.TriggerGenerationForAllSizes(restoredImage)
		return restoredImage, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 验证文件类型
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

	// 获取图片尺寸
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}
	imgWidth, imgHeight := utils.GetImageDimensions(tempFile)

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
		Width:           imgWidth,
		Height:          imgHeight,
		IsPublic:        isPublic,
		UserID:          userID,
	}

	if err := s.repo.SaveImage(newImage); err != nil {
		storageProvider.DeleteWithContext(ctx, identifier)
		return nil, false, errors.New("failed to save image metadata")
	}

	go s.warmCache(newImage)
	go s.converter.TriggerWebPConversion(newImage)
	go s.thumbnailService.TriggerGenerationForAllSizes(newImage)

	return newImage, false, nil
}

// getStorageProvider 获取存储提供者
func (s *UploadService) getStorageProvider(storageName string) (storage.Provider, uint, error) {
	if storageName != "" {
		provider, err := s.storageFactory.Get(storageName)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid storage configuration: %w", err)
		}
		id, err := s.storageFactory.GetIDByName(storageName)
		if err != nil {
			return nil, 0, err
		}
		return provider, id, nil
	}

	provider := s.storageFactory.GetDefault()
	if provider == nil {
		return nil, 0, errors.New("no default storage configured")
	}
	return provider, s.storageFactory.GetDefaultID(), nil
}

// warmCache 预热缓存
func (s *UploadService) warmCache(image *models.Image) {
	if s.cacheHelper == nil {
		return
	}
	ctx := context.Background()
	if err := s.cacheHelper.CacheImage(ctx, image); err != nil {
		// 只记录日志
	}
}
