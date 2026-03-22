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
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/worker"
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
	albumsRepo     *albums.Repository
	converter      *Converter
	thumbnailSvc   *ThumbnailService
	variantService *VariantService
	cacheHelper    *cache.Helper
	configManager  *dbconfig.Manager
	baseURL        string
	pathGenerator  *generator.PathGenerator
}

// NewService 创建图片服务
func NewService(
	repo *images.Repository,
	variantRepo *images.VariantRepository,
	albumsRepo *albums.Repository,
	converter *Converter,
	thumbnailSvc *ThumbnailService,
	variantService *VariantService,
	cacheHelper *cache.Helper,
	configManager *dbconfig.Manager,
	baseURL string,
) *Service {
	return &Service{
		repo:           repo,
		variantRepo:    variantRepo,
		albumsRepo:     albumsRepo,
		converter:      converter,
		thumbnailSvc:   thumbnailSvc,
		variantService: variantService,
		cacheHelper:    cacheHelper,
		configManager:  configManager,
		baseURL:        baseURL,
		pathGenerator:  generator.NewPathGenerator(),
	}
}

// submitBackgroundTask 提交后台任务到 worker pool，队列满时 fallback 到新 goroutine
func (s *Service) submitBackgroundTask(task func()) {
	if pool := worker.GetGlobalPool(); pool != nil {
		if ok := pool.Submit(task); !ok {
			utils.LogIfDevf("[Service] Worker pool queue full, running task in new goroutine")
			go task()
		}
	} else {
		utils.LogIfDevf("[Service] Worker pool not initialized, running task in new goroutine")
		go task()
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
	defaultAlbumID uint,
) (*UploadResult, error) {
	storageProvider, err := s.getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	image, isDup, err := s.processAndSaveImage(ctx, userID, fileHeader, storageProvider, storageID, isPublic, defaultAlbumID)
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
func (s *Service) UploadBatch(ctx context.Context, userID uint, files []*multipart.FileHeader, storageID uint, isPublic bool, defaultAlbumID uint, concurrentLimit int) ([]*UploadResult, error) {
	storageProvider, err := s.getStorageProviderByID(storageID)
	if err != nil {
		return nil, err
	}

	if concurrentLimit <= 0 {
		concurrentLimit = 3
	}

	results := make([]*UploadResult, len(files))
	var resultsMutex sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	// 使用信号量限制并发数
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
func (s *Service) processAndSaveImage(ctx context.Context, userID uint, fileHeader *multipart.FileHeader, storageProvider storage.Provider, storageConfigID uint, isPublic bool, defaultAlbumID uint) (*models.Image, bool, error) {
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

	tmp, err := os.CreateTemp(config.TempDir, "upload-*")
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

	img, err := s.repo.GetImageByHash(fileHash)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, errors.New("database error during hash check")
	}

	if err == nil && img.DeletedAt.Valid {
		updates := map[string]any{
			"deleted_at":        nil,
			"original_name":     fileHeader.Filename,
			"user_id":           userID,
			"is_public":         isPublic,
			"storage_config_id": storageConfigID,
		}
		restored, err := s.repo.UpdateImageByIdentifier(img.Identifier, updates)
		if err != nil {
			return nil, false, errors.New("failed to restore existing image data")
		}

		s.submitBackgroundTask(func() { s.warmCache(restored) })
		if s.converter != nil {
			s.submitBackgroundTask(func() { s.converter.TriggerConversion(restored) })
		}

		return restored, true, nil
	}

	if err == nil {
		// 如果是其他用户的图片，创建新的逻辑记录（物理去重 + 逻辑隔离）
		if img.UserID != userID {
			newImg, err := s.createDedupedImageRecord(img, userID, fileHeader.Filename, storageConfigID, isPublic)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create deduped image record: %w", err)
			}
			s.submitBackgroundTask(func() { s.warmCache(newImg) })
			return newImg, true, nil
		}

		// 同一用户重复上传，返回原记录
		s.submitBackgroundTask(func() { s.warmCache(img) })
		if s.converter != nil {
			s.submitBackgroundTask(func() { s.converter.TriggerConversion(img) })
		}
		return img, true, nil
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file: %w", err)
	}

	width, height := utils.GetImageDimensions(tmp)

	// GetImageDimensions 会移动文件指针，保存前必须重置到开头
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("failed to seek temp file after dimension extraction: %w", err)
	}

	ext := getSafeFileExtension(mimeType)
	ids := s.pathGenerator.GenerateOriginalIdentifiers(fileHash, ext, time.Now())
	identifier := ids.Identifier
	storagePath := ids.StoragePath
	if err := storageProvider.SaveWithContext(ctx, storagePath, tmp); err != nil {
		return nil, false, errors.New("failed to save uploaded file")
	}

	fileInfo, err := tmp.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("failed to get file size: %w", err)
	}
	actualFileSize := fileInfo.Size()

	newImg := &models.Image{
		Identifier:      identifier,
		StoragePath:     storagePath,
		OriginalName:    fileHeader.Filename,
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
			log.Printf("[Service] Failed to add image to default album %d: %v", defaultAlbumID, err)
		}
	}

	s.submitBackgroundTask(func() { s.warmCache(newImg) })
	if s.converter != nil {
		s.submitBackgroundTask(func() { s.converter.TriggerConversion(newImg) })
	}

	return newImg, false, nil
}

// createDedupedImageRecord 为不同用户创建去重后的新图片记录
// 物理文件复用，但逻辑上属于不同用户（物理去重 + 逻辑隔离）
func (s *Service) createDedupedImageRecord(existing *models.Image, userID uint, originalName string, storageConfigID uint, isPublic bool) (*models.Image, error) {
	// 生成新的 identifier（使用时间戳确保唯一性）
	ids := s.pathGenerator.GenerateOriginalIdentifiers(existing.FileHash+fmt.Sprintf("_%d", userID), filepath.Ext(originalName), time.Now())

	newImg := &models.Image{
		Identifier:      ids.Identifier,
		StoragePath:     existing.StoragePath, // 复用相同的物理文件路径
		OriginalName:    originalName,
		FileSize:        existing.FileSize,
		MimeType:        existing.MimeType,
		StorageConfigID: storageConfigID,
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
	_ = s.cacheHelper.CacheImage(ctx, image)
}

// GetImageMetadata 获取图片元数据
func (s *Service) GetImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
	var image models.Image

	if err := s.cacheHelper.GetCachedImage(ctx, identifier, &image); err == nil {
		return &image, nil
	}

	resultChan := imageGroup.DoChan(identifier, func() (any, error) {
		imagePtr, err := s.repo.GetImageByIdentifier(identifier)
		if err != nil {
			if isTransientError(err) {
				return nil, ErrTemporaryFailure
			}
			return nil, err
		}

		// 异步写入缓存（使用独立上下文，避免 HTTP 请求取消影响后台任务）
		go func(img *models.Image) {
			defer func() {
				if r := recover(); r != nil {
					utils.LogIfDevf("Panic in async cache goroutine for '%s': %v", img.Identifier, r)
				}
			}()
			if s.cacheHelper == nil {
				return
			}
			// 使用独立上下文，不依赖可能已被取消的 HTTP 请求上下文
			cacheCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cacheErr := s.cacheHelper.CacheImage(cacheCtx, img); cacheErr != nil {
				utils.LogIfDevf("Failed to cache image metadata for '%s': %v", img.Identifier, cacheErr)
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

// GetImageWithNegotiation 获取图片
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
func (s *Service) ListImages(storageType string, identifier string, search string, albumID *uint, startTime, endTime int64, sort string, page int, limit int, userID int) (*ListImagesResult, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = config.DefaultPerPage
	}

	// 限制最大分页数量
	if limit > config.MaxPerPage {
		limit = config.MaxPerPage
	}

	list, total, err := s.repo.GetImageList(storageType, identifier, search, albumID, startTime, endTime, sort, page, limit, userID)
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

	// Check context errors
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// Check for net.Error (timeout / temporary)
	var netErr interface {
		Timeout() bool
	}
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Check for os-level DNS / connection errors
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	return false
}

// DeleteSingle 删除单张图片
func (s *Service) DeleteSingle(ctx context.Context, identifier string, userID uint) (*DeleteResult, error) {
	img, err := s.repo.GetImageByIdentifier(identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &DeleteResult{Success: false, Error: errors.New("image not found")}, nil
		}
		return nil, fmt.Errorf("failed to get image info: %w", err)
	}

	if img.UserID != userID {
		return &DeleteResult{Success: false, Error: errors.New("permission denied")}, nil
	}

	// 从所有相册中移除关联
	if err := s.repo.RemoveImageFromAllAlbums(img.ID); err != nil {
		log.Printf("Failed to remove image %d from albums: %v", img.ID, err)
	}

	// 先删除变体，再删除原图文件，最后删除DB记录（避免幽灵文件）
	s.deleteVariantsForImage(ctx, img)

	// 检查是否还有其他图片引用同一个物理文件（秒传引用计数）
	if img.StoragePath != "" {
		refCount, err := s.repo.CountImagesByStoragePath(img.StoragePath)
		if err != nil {
			log.Printf("Failed to count references for storage path %s: %v", img.StoragePath, err)
		} else if refCount <= 1 {
			// 只有当前这一条记录引用，可以安全删除物理文件
			provider, err := s.getStorageProviderByID(img.StorageConfigID)
			if err != nil {
				log.Printf("Failed to get storage provider for image %s: %v", utils.SanitizeLogMessage(img.Identifier), err)
			} else if err := provider.DeleteWithContext(ctx, img.StoragePath); err != nil {
				log.Printf("Failed to delete original image file %s: %v", img.StoragePath, err)
			}
		} else {
			utils.LogIfDevf("[Delete] Skipping physical file deletion for %s, still referenced by %d images", img.StoragePath, refCount-1)
		}
	}

	if err := s.repo.DeleteImageByIdentifierAndUser(identifier, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &DeleteResult{Success: false, Error: errors.New("image not found")}, nil
		}
		return nil, fmt.Errorf("failed to delete image: %w", err)
	}

	// 清除缓存
	s.clearImageCache(ctx, identifier)

	return &DeleteResult{Success: true, DeletedCount: 1}, nil
}

// DeleteBatch 批量删除图片
func (s *Service) DeleteBatch(ctx context.Context, identifiers []string, userID uint) (*DeleteResult, error) {
	if len(identifiers) == 0 {
		return &DeleteResult{Success: true, DeletedCount: 0}, nil
	}

	// 使用事务执行所有数据库删除操作，确保数据一致性
	result, imagesToDelete, err := s.repo.DeleteBatchTransaction(ctx, identifiers, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch delete: %w", err)
	}

	// 删除变体的物理文件（在事务外执行，避免影响数据库一致性）
	for _, img := range imagesToDelete {
		s.deleteVariantsForImage(ctx, img)
	}

	// 检查每个被删除图片的物理文件引用计数
	// 只有没有其他引用时才删除物理文件（避免秒传共享文件被误删）
	for _, img := range imagesToDelete {
		if img.StoragePath != "" {
			refCount, err := s.repo.CountImagesByStoragePath(img.StoragePath)
			if err != nil {
				log.Printf("Failed to count references for storage path %s: %v", img.StoragePath, err)
				continue
			}
			if refCount == 0 {
				provider, err := s.getStorageProviderByID(img.StorageConfigID)
				if err != nil {
					log.Printf("Failed to get storage provider for image %s: %v", utils.SanitizeLogMessage(img.Identifier), err)
				} else if err := provider.DeleteWithContext(ctx, img.StoragePath); err != nil {
					log.Printf("Failed to delete original image file %s: %v", img.StoragePath, err)
				}
			} else {
				utils.LogIfDevf("[DeleteBatch] Skipping physical file deletion for %s, still referenced by %d images", img.StoragePath, refCount)
			}
		}
	}

	// 清除缓存
	for _, identifier := range identifiers {
		s.clearImageCache(ctx, identifier)
	}

	return &DeleteResult{Success: true, DeletedCount: result.DeletedCount}, nil
}

// DeleteImageVariants 删除指定图片的所有变体
func (s *Service) DeleteImageVariants(ctx context.Context, img *models.Image) error {
	s.deleteVariantsForImage(ctx, img)
	return nil
}

// ClearImageCache 清除指定图片的缓存
func (s *Service) ClearImageCache(ctx context.Context, identifier string) error {
	s.clearImageCache(ctx, identifier)
	return nil
}

func (s *Service) deleteVariantsForImage(ctx context.Context, img *models.Image) {
	variants, err := s.variantRepo.GetVariantsByImageID(img.ID)
	if err != nil {
		log.Printf("Failed to get variants for image %d: %v", img.ID, err)
		return
	}

	// 获取图片对应的 storage provider
	provider, err := s.getStorageProviderByID(img.StorageConfigID)
	if err != nil {
		log.Printf("Failed to get storage provider for image %d: %v", img.ID, err)
		return
	}

	for _, variant := range variants {
		if variant.StoragePath == "" || variant.Status != models.VariantStatusCompleted {
			continue
		}

		if err := provider.DeleteWithContext(ctx, variant.StoragePath); err != nil {
			log.Printf("Failed to delete variant file %s: %v", variant.StoragePath, err)
		}

		if err := s.cacheHelper.DeleteCachedImageData(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete cache for variant %s: %v", utils.SanitizeLogMessage(variant.Identifier), err)
		}
	}

	if err := s.variantRepo.DeleteByImageID(img.ID); err != nil {
		log.Printf("Failed to delete variant records for image %d: %v", img.ID, err)
	}
}

func (s *Service) clearImageCache(ctx context.Context, identifier string) {
	if err := s.cacheHelper.DeleteCachedImage(ctx, identifier); err != nil {
		log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
	if err := s.cacheHelper.DeleteCachedImageData(ctx, identifier); err != nil {
		log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
}

// GetImageByIdentifier 获取图片
func (s *Service) GetImageByIdentifier(identifier string) (*models.Image, error) {
	return s.repo.GetImageByIdentifier(identifier)
}

// UpdateImageByIdentifier 更新图片
func (s *Service) UpdateImageByIdentifier(identifier string, updates map[string]any) (*models.Image, error) {
	return s.repo.UpdateImageByIdentifier(identifier, updates)
}

// GetImageWithVariant 获取图片
func (s *Service) GetImageWithVariant(ctx context.Context, identifier string, acceptHeader string, userID uint) (*ImageResultDTO, error) {
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
		return &ImageResultDTO{
			Image:      image,
			IsOriginal: true,
			URL:        utils.BuildImageURL(s.baseURL, image.Identifier),
			MIMEType:   image.MimeType,
		}, nil
	}

	if !variantResult.IsOriginal && variantResult.Variant == nil && s.converter != nil {
		s.submitBackgroundTask(func() {
			s.converter.TriggerConversion(image)
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

			result.IsOriginal = true
			result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
			result.MIMEType = image.MimeType
		}
	}

	return result, nil
}

// GetRandomImage 获取随机图片
func (s *Service) GetRandomImage(filter *images.RandomImageFilter) (*models.Image, error) {
	return s.repo.GetRandomPublicImage(filter)
}

// GetRandomImageWithVariant 获取随机图片
func (s *Service) GetRandomImageWithVariant(ctx context.Context, filter *images.RandomImageFilter, acceptHeader string) (*ImageResultDTO, error) {
	// 获取随机图片
	image, err := s.repo.GetRandomPublicImage(filter)
	if err != nil {
		return nil, err
	}

	// 选择最优变体
	variantResult, err := s.variantService.SelectBestVariant(ctx, image, acceptHeader)
	if err != nil {
		return &ImageResultDTO{
			Image:      image,
			IsOriginal: true,
			URL:        utils.BuildImageURL(s.baseURL, image.Identifier),
			MIMEType:   image.MimeType,
		}, nil
	}

	if !variantResult.IsOriginal && variantResult.Variant == nil && s.converter != nil {
		s.submitBackgroundTask(func() {
			s.converter.TriggerConversion(image)
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
			result.IsOriginal = true
			result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
			result.MIMEType = image.MimeType
		}
	}

	return result, nil
}
