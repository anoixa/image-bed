package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"golang.org/x/sync/singleflight"
)

var (
	imageGroup       singleflight.Group
	metaFetchTimeout = 30 * time.Second
)

var (
	ErrTemporaryFailure = errors.New("temporary failure, should be retried")
)

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

// QueryService 图片查询服务
type QueryService struct {
	repo           images.RepositoryInterface
	variantRepo    images.VariantRepository
	storageFactory *storage.Factory
	cacheHelper    *cache.Helper
	variantService *VariantService
}

// NewQueryService 创建查询服务
func NewQueryService(
	repo images.RepositoryInterface,
	variantRepo images.VariantRepository,
	storageFactory *storage.Factory,
	cacheHelper *cache.Helper,
	variantService *VariantService,
) *QueryService {
	return &QueryService{
		repo:           repo,
		variantRepo:    variantRepo,
		storageFactory: storageFactory,
		cacheHelper:    cacheHelper,
		variantService: variantService,
	}
}

// GetImageMetadata 获取图片元数据（带缓存和 singleflight）
func (s *QueryService) GetImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
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
			cacheCtx := context.Background()
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
func (s *QueryService) CheckImagePermission(image *models.Image, userID uint) bool {
	if image.IsPublic {
		return true
	}
	return userID != 0 && userID == image.UserID
}

// GetImageWithNegotiation 获取图片（支持格式协商）
func (s *QueryService) GetImageWithNegotiation(
	ctx context.Context,
	image *models.Image,
	acceptHeader string,
) (*VariantResult, error) {
	return s.variantService.SelectBestVariant(ctx, image, acceptHeader)
}

// GetCachedImageData 获取缓存的图片数据
func (s *QueryService) GetCachedImageData(ctx context.Context, identifier string) ([]byte, error) {
	return s.cacheHelper.GetCachedImageData(ctx, identifier)
}

// GetImageStream 从存储获取图片流
func (s *QueryService) GetImageStream(ctx context.Context, image *models.Image) (io.ReadSeeker, error) {
	storageProvider, err := s.storageFactory.GetByID(image.StorageConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider: %w", err)
	}

	return storageProvider.GetWithContext(ctx, image.Identifier)
}

// GetVariantStream 从存储获取变体流
func (s *QueryService) GetVariantStream(ctx context.Context, storageConfigID uint, identifier string) (io.ReadSeeker, error) {
	storageProvider, err := s.storageFactory.GetByID(storageConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage provider: %w", err)
	}

	return storageProvider.GetWithContext(ctx, identifier)
}

// CacheImageData 缓存图片数据
func (s *QueryService) CacheImageData(ctx context.Context, identifier string, data []byte) error {
	if s.cacheHelper == nil {
		return nil
	}
	return s.cacheHelper.CacheImageData(ctx, identifier, data)
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

// ListImagesResult 图片列表结果
type ListImagesResult struct {
	Images     []*models.Image
	Total      int64
	Page       int
	Limit      int
	TotalPages int
}

// ListImages 获取图片列表
func (s *QueryService) ListImages(
	storageType string,
	identifier string,
	search string,
	albumID *uint,
	page int,
	limit int,
	userID int,
) (*ListImagesResult, error) {
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

	list, total, err := s.repo.GetImageList(storageType, identifier, search, albumID, page, limit, userID)
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
func (s *QueryService) IsTransientError(err error) bool {
	return isTransientError(err)
}

// ForgetSingleflight 忘记 singleflight 键
func (s *QueryService) ForgetSingleflight(identifier string) {
	imageGroup.Forget(identifier)
}
