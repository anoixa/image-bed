package image

import (
	"bytes"
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
	"strings"
	"sync"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/anoixa/image-bed/utils/pool"
	"github.com/anoixa/image-bed/utils/validator"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
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

// ImageStreamResult 图片流结果
type ImageStreamResult struct {
	Data       []byte
	MIMEType   string
	FileSize   int64
	Identifier string
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

// Service 图片服务
type Service struct {
	repo           *images.Repository
	variantRepo    *images.VariantRepository
	converter      *Converter
	thumbnailSvc   *ThumbnailService
	variantService *VariantService
	cacheHelper    *cache.Helper
	baseURL        string
}

// NewService 创建图片服务
func NewService(
	repo *images.Repository,
	variantRepo *images.VariantRepository,
	converter *Converter,
	thumbnailSvc *ThumbnailService,
	variantService *VariantService,
	cacheHelper *cache.Helper,
	baseURL string,
) *Service {
	return &Service{
		repo:           repo,
		variantRepo:    variantRepo,
		converter:      converter,
		thumbnailSvc:   thumbnailSvc,
		variantService: variantService,
		cacheHelper:    cacheHelper,
		baseURL:        baseURL,
	}
}

// getSafeFileExtension 根据MIME类型获取安全的文件扩展名
func getSafeFileExtension(mimeType string) string {
	ext := utils.GetSafeExtension(mimeType)
	if ext == "" {
		return ".bin"
	}
	return ext
}


// UploadSingle 单文件上传
func (s *Service) UploadSingle(
	ctx context.Context,
	userID uint,
	fileHeader *multipart.FileHeader,
	storageID uint,
	isPublic bool,
) (*UploadResult, error) {
	storageProvider, err := s.getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	image, isDup, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageID, isPublic)
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
func (s *Service) UploadBatch(ctx context.Context, userID uint, files []*multipart.FileHeader, storageID uint, isPublic bool) ([]*UploadResult, error) {
	storageProvider, err := s.getStorageProviderByID(storageID)
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
				image, _, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageID, isPublic)
				result := &UploadResult{
					FileName: fileHeader.Filename,
				}

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

// UploadSingleWithName 单文件上传（使用存储名称，向后兼容）
func (s *Service) UploadSingleWithName(ctx context.Context, userID uint, fileHeader *multipart.FileHeader, storageName string, isPublic bool) (*UploadResult, error) {
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
		Links:       utils.BuildLinkFormats(s.baseURL, image.Identifier),
	}, nil
}

// UploadBatchWithName 批量上传（使用存储名称，向后兼容）
func (s *Service) UploadBatchWithName(
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
func (s *Service) processAndSaveImage(ctx context.Context, userID uint, fileHeader *multipart.FileHeader, storageProvider storage.Provider, storageConfigID uint, isPublic bool) (*models.Image, bool, error) {
	src, err := fileHeader.Open()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = src.Close() }()

	// 预读头部用于 MIME 验证
	header := make([]byte, 512)
	n, _ := io.ReadFull(src, header)
	header = header[:n]

	isImage, mimeType := validator.IsImageBytes(header)
	if !isImage {
		return nil, false, errors.New("the uploaded file type is not supported")
	}

	reader := io.MultiReader(bytes.NewReader(header), src)

	// 创建临时文件
	tmp, err := os.CreateTemp("./data/temp", "upload-*")
	if err != nil {
		return nil, false, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// 同时计算哈希并写入临时文件
	hash := sha256.New()
	w := io.MultiWriter(tmp, hash)

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)
	buf := *bufPtr

	if _, err = io.CopyBuffer(w, reader, buf); err != nil {
		return nil, false, fmt.Errorf("failed to process file stream: %w", err)
	}

	fileHash := hex.EncodeToString(hash.Sum(nil))

	// 检查重复文件
	if img, err := s.repo.GetImageByHash(fileHash); err == nil {
		go s.warmCache(img)
		go s.converter.TriggerWebPConversion(img)
		go s.thumbnailSvc.TriggerGenerationForAllSizes(img)
		return img, true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 检查软删除的文件
	if softDeleted, err := s.repo.GetSoftDeletedImageByHash(fileHash); err == nil {
		updates := map[string]interface{}{
			"deleted_at":        nil,
			"original_name":     fileHeader.Filename,
			"user_id":           userID,
			"is_public":         isPublic,
			"storage_config_id": storageConfigID,
		}
		restored, err := s.repo.UpdateImageByIdentifier(softDeleted.Identifier, updates)
		if err != nil {
			return nil, false, errors.New("failed to restore existing image data")
		}
		go s.warmCache(restored)
		go s.converter.TriggerWebPConversion(restored)
		go s.thumbnailSvc.TriggerGenerationForAllSizes(restored)
		return restored, true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	// 只需一次 Seek 获取尺寸并保存
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}

	width, height := utils.GetImageDimensions(tmp)

	// 重新定位到开头用于保存
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}

	// 使用 PathGenerator 生成时间分层路径
	pg := generator.NewPathGenerator()
	ext := getSafeFileExtension(mimeType)
	ids := pg.GenerateOriginalIdentifiers(fileHash, ext, time.Now())
	identifier := ids.Identifier
	storagePath := ids.StoragePath
	if err := storageProvider.SaveWithContext(ctx, storagePath, tmp); err != nil {
		return nil, false, errors.New("failed to save uploaded file")
	}

	// 创建数据库记录
	img := &models.Image{
		Identifier:      identifier,
		StoragePath:     storagePath,
		OriginalName:    fileHeader.Filename,
		FileSize:        fileHeader.Size,
		MimeType:        mimeType,
		StorageConfigID: storageConfigID,
		FileHash:        fileHash,
		Width:           width,
		Height:          height,
		IsPublic:        isPublic,
		UserID:          userID,
	}

	if err := s.repo.SaveImage(img); err != nil {
		_ = storageProvider.DeleteWithContext(ctx, storagePath)
		return nil, false, errors.New("failed to save image metadata")
	}

	go s.warmCache(img)
	go s.converter.TriggerWebPConversion(img)
	go s.thumbnailSvc.TriggerGenerationForAllSizes(img)

	return img, false, nil
}

func (s *Service) getStorageProvider(storageName string) (storage.Provider, uint, error) {
	provider := storage.GetDefault()
	if provider == nil {
		return nil, 0, errors.New("no default storage configured")
	}
	return provider, storage.GetDefaultID(), nil
}

// getStorageProviderByID 根据存储ID获取存储提供者
func (s *Service) getStorageProviderByID(storageID uint) (storage.Provider, error) {
	// storageID 为 0 表示使用默认存储
	if storageID == 0 {
		provider := storage.GetDefault()
		if provider == nil {
			return nil, errors.New("no default storage configured")
		}
		return provider, nil
	}

	// 根据 ID 获取存储提供者
	provider, err := storage.GetByID(storageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider by ID %d: %w", storageID, err)
	}
	return provider, nil
}

func (s *Service) warmCache(image *models.Image) {
	if s.cacheHelper == nil {
		return
	}
	ctx := context.Background()
	_ = s.cacheHelper.CacheImage(ctx, image) // 缓存失败只记录日志
}

// GetImageMetadata 获取图片元数据
func (s *Service) GetImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
	var image models.Image

	// 尝试从缓存获取
	if err := s.cacheHelper.GetCachedImage(ctx, identifier, &image); err == nil {
		return &image, nil
	}

	resultChan := imageGroup.DoChan(identifier, func() (interface{}, error) {
		imagePtr, err := s.repo.GetImageByIdentifier(identifier)
		if err != nil {
			if isTransientError(err) {
				return nil, ErrTemporaryFailure
			}
			return nil, err
		}

		// 异步写入缓存
		go func(img *models.Image) {
			if s.cacheHelper == nil {
				return
			}
			cacheCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cacheErr := s.cacheHelper.CacheImage(cacheCtx, img); cacheErr != nil {
				log.Printf("Failed to cache image metadata for '%s': %v", img.Identifier, cacheErr)
			}
		}(imagePtr)

		return imagePtr, nil
	})

	select {
	case result := <-resultChan:
		if result.Err != nil {
			if errors.Is(result.Err, ErrTemporaryFailure) {
				imageGroup.Forget(identifier)
			}
			return nil, result.Err
		}
		return result.Val.(*models.Image), nil
	case <-time.After(metaFetchTimeout):
		imageGroup.Forget(identifier)
		return nil, ErrTemporaryFailure
	}
}

// CheckImagePermission 检查图片访问权限
func (s *Service) CheckImagePermission(image *models.Image, userID uint) bool {
	if image.IsPublic {
		return true
	}
	return userID != 0 && userID == image.UserID
}

// GetImageWithNegotiation 获取图片（支持格式协商）
func (s *Service) GetImageWithNegotiation(ctx context.Context, image *models.Image, acceptHeader string) (*VariantResult, error) {
	return s.variantService.SelectBestVariant(ctx, image, acceptHeader)
}

// GetCachedImageData 获取缓存的图片数据
func (s *Service) GetCachedImageData(ctx context.Context, identifier string) ([]byte, error) {
	return s.cacheHelper.GetCachedImageData(ctx, identifier)
}

// GetImageStream 从存储获取图片流
func (s *Service) GetImageStream(ctx context.Context, image *models.Image) (io.ReadSeeker, error) {
	provider, err := s.getStorageProviderByID(image.StorageConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider: %w", err)
	}
	return provider.GetWithContext(ctx, image.StoragePath)
}

// GetVariantStream 从存储获取变体流
func (s *Service) GetVariantStream(ctx context.Context, storageConfigID uint, storagePath string) (io.ReadSeeker, error) {
	provider, err := s.getStorageProviderByID(storageConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider: %w", err)
	}
	return provider.GetWithContext(ctx, storagePath)
}

// CacheImageData 缓存图片数据
func (s *Service) CacheImageData(ctx context.Context, identifier string, data []byte) error {
	if s.cacheHelper == nil {
		return nil
	}
	return s.cacheHelper.CacheImageData(ctx, identifier, data)
}

// ListImages 获取图片列表
func (s *Service) ListImages(storageType string, identifier string, search string, albumID *uint, startTime, endTime int64, page int, limit int, userID int) (*ListImagesResult, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}

	// 限制最大分页数量
	const maxLimit = 100
	if limit > maxLimit {
		limit = maxLimit
	}

	list, total, err := s.repo.GetImageList(storageType, identifier, search, albumID, startTime, endTime, page, limit, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get image list: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}

	return &ListImagesResult{
		Images:     list,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}, nil
}

// IsTransientError 暴露临时错误检查方法
func (s *Service) IsTransientError(err error) bool {
	return isTransientError(err)
}

// ForgetSingleflight 忘记 singleflight 键
func (s *Service) ForgetSingleflight(identifier string) {
	imageGroup.Forget(identifier)
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	timeoutPatterns := []string{
		"timeout",
		"deadline exceeded",
		"connection refused",
		"connection reset",
		"temporary",
		"i/o timeout",
		"context deadline exceeded",
		"connection timed out",
		"no such host",
		"network is unreachable",
	}

	for _, pattern := range timeoutPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}
	return false
}

// DeleteSingle 删除单张图片
func (s *Service) DeleteSingle(ctx context.Context, identifier string, userID uint) (*DeleteResult, error) {
	// 获取图片信息
	img, err := s.repo.GetImageByIdentifier(identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &DeleteResult{Success: false, Error: errors.New("image not found")}, nil
		}
		return nil, fmt.Errorf("failed to get image info: %w", err)
	}

	// 检查权限
	if img.UserID != userID {
		return &DeleteResult{Success: false, Error: errors.New("permission denied")}, nil
	}

	// 删除数据库记录
	if err := s.repo.DeleteImageByIdentifierAndUser(identifier, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &DeleteResult{Success: false, Error: errors.New("image not found")}, nil
		}
		return nil, fmt.Errorf("failed to delete image: %w", err)
	}

	// 级联删除变体
	deleteVariantsForImage(ctx, s.variantRepo, s.cacheHelper, img)

	// 清除缓存
	clearImageCache(ctx, s.cacheHelper, identifier)

	return &DeleteResult{Success: true, DeletedCount: 1}, nil
}

// DeleteBatch 批量删除图片
func (s *Service) DeleteBatch(ctx context.Context, identifiers []string, userID uint) (*DeleteResult, error) {
	if len(identifiers) == 0 {
		return &DeleteResult{Success: true, DeletedCount: 0}, nil
	}

	// 批量查询图片信息（避免 N+1 查询）
	imagesToDelete, err := s.repo.GetImagesByIdentifiersAndUser(identifiers, userID)
	if err != nil {
		log.Printf("Failed to get images for batch delete: %v", err)
	}

	// 级联删除变体
	for _, img := range imagesToDelete {
		deleteVariantsForImage(ctx, s.variantRepo, s.cacheHelper, img)
	}

	// 删除数据库记录
	affectedCount, err := s.repo.DeleteImagesByIdentifiersAndUser(identifiers, userID)
	if err != nil {
		log.Printf("Failed to delete image records: %v", err)
	}

	// 清除缓存
	for _, identifier := range identifiers {
		clearImageCache(ctx, s.cacheHelper, identifier)
	}

	return &DeleteResult{Success: true, DeletedCount: affectedCount}, nil
}

// DeleteImageVariants 删除指定图片的所有变体（公开方法）
func (s *Service) DeleteImageVariants(ctx context.Context, img *models.Image) error {
	deleteVariantsForImage(ctx, s.variantRepo, s.cacheHelper, img)
	return nil
}

// ClearImageCache 清除指定图片的缓存（公开方法）
func (s *Service) ClearImageCache(ctx context.Context, identifier string) error {
	clearImageCache(ctx, s.cacheHelper, identifier)
	return nil
}

func deleteVariantsForImage(ctx context.Context, variantRepo *images.VariantRepository, cacheHelper *cache.Helper, img *models.Image) {
	// 获取所有变体
	variants, err := variantRepo.GetVariantsByImageID(img.ID)
	if err != nil {
		log.Printf("Failed to get variants for image %d: %v", img.ID, err)
		return
	}

	// 删除每个变体的文件和缓存
	for _, variant := range variants {
		if variant.Identifier == "" || variant.Status != models.VariantStatusCompleted {
			continue
		}

		if err := storage.GetDefault().DeleteWithContext(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete variant file %s: %v", variant.Identifier, err)
		}

		if err := cacheHelper.DeleteCachedImageData(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete cache for variant %s: %v", utils.SanitizeLogMessage(variant.Identifier), err)
		}
	}

	// 删除数据库中的变体记录
	if err := variantRepo.DeleteByImageID(img.ID); err != nil {
		log.Printf("Failed to delete variant records for image %d: %v", img.ID, err)
	}
}

func clearImageCache(ctx context.Context, cacheHelper *cache.Helper, identifier string) {
	if err := cacheHelper.DeleteCachedImage(ctx, identifier); err != nil {
		log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
	if err := cacheHelper.DeleteCachedImageData(ctx, identifier); err != nil {
		log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
}

// ServeImageData 提供图片数据
func ServeImageData(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// StreamImage 流式传输图片
func StreamImage(ctx context.Context, w http.ResponseWriter, reader io.ReadSeeker, contentType string, fileSize int64) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
	w.WriteHeader(http.StatusOK)

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)
	buf := *bufPtr

	_, err := io.CopyBuffer(w, reader, buf)
	return err
}

// ReadImageData 读取图片数据
func ReadImageData(reader io.ReadSeeker) ([]byte, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GetImageByIdentifier 获取图片
func (s *Service) GetImageByIdentifier(identifier string) (*models.Image, error) {
	return s.repo.GetImageByIdentifier(identifier)
}

// UpdateImageByIdentifier 更新图片
func (s *Service) UpdateImageByIdentifier(identifier string, updates map[string]interface{}) (*models.Image, error) {
	return s.repo.UpdateImageByIdentifier(identifier, updates)
}

// GetImageWithVariant 获取图片（包含完整的业务逻辑）
func (s *Service) GetImageWithVariant(ctx context.Context, identifier string, acceptHeader string, userID uint) (*ImageResultDTO, error) {
	// 使用 GetImageMetadata 以启用缓存和防击穿
	image, err := s.GetImageMetadata(ctx, identifier)
	if err != nil {
		return nil, err
	}

	if !image.IsPublic && image.UserID != userID {
		return nil, ErrForbidden
	}

	// 选择最优变体
	variantResult, err := s.variantService.SelectBestVariant(ctx, image, acceptHeader)
	if err != nil {
		// 失败时返回原图
		return &ImageResultDTO{
			Image:      image,
			IsOriginal: true,
			URL:        utils.BuildImageURL(s.baseURL, image.Identifier),
			MIMEType:   image.MimeType,
		}, nil
	}

	if !variantResult.IsOriginal && variantResult.Variant == nil {
		utils.SafeGo(func() {
			s.converter.TriggerWebPConversion(image)
		})
	}

	result := &ImageResultDTO{
		Image:      image,
		IsOriginal: variantResult.IsOriginal,
		MIMEType:   variantResult.MIMEType,
	}

	if variantResult.IsOriginal {
		result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
		result.MIMEType = image.MimeType
	} else {
		result.Variant = variantResult.Variant
		if variantResult.Variant != nil {
			result.URL = utils.BuildImageURL(s.baseURL, variantResult.Variant.Identifier)
		} else {
			// 变体不存在，降级返回原图
			result.IsOriginal = true
			result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
			result.MIMEType = image.MimeType
		}
	}

	return result, nil
}

