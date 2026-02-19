package image

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

// DeleteResult 删除结果
type DeleteResult struct {
	Success      bool
	DeletedCount int64
	Error        error
}

// DeleteService 图片删除服务
type DeleteService struct {
	repo           images.RepositoryInterface
	variantRepo    images.VariantRepository
	storageFactory *storage.Factory
	cacheHelper    *cache.Helper
}

// NewDeleteService 创建删除服务
func NewDeleteService(
	repo images.RepositoryInterface,
	variantRepo images.VariantRepository,
	storageFactory *storage.Factory,
	cacheHelper *cache.Helper,
) *DeleteService {
	return &DeleteService{
		repo:           repo,
		variantRepo:    variantRepo,
		storageFactory: storageFactory,
		cacheHelper:    cacheHelper,
	}
}

// DeleteSingle 删除单张图片
func (s *DeleteService) DeleteSingle(ctx context.Context, identifier string, userID uint) (*DeleteResult, error) {
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
	s.deleteVariantsForImage(ctx, img)

	// 清除缓存
	s.clearImageCache(ctx, identifier)

	return &DeleteResult{Success: true, DeletedCount: 1}, nil
}

// DeleteBatch 批量删除图片
func (s *DeleteService) DeleteBatch(ctx context.Context, identifiers []string, userID uint) (*DeleteResult, error) {
	if len(identifiers) == 0 {
		return &DeleteResult{Success: true, DeletedCount: 0}, nil
	}

	// 获取所有要删除的图片信息（用于级联删除变体）
	var imagesToDelete []*models.Image
	for _, identifier := range identifiers {
		img, err := s.repo.GetImageByIdentifier(identifier)
		if err == nil && img != nil && img.UserID == userID {
			imagesToDelete = append(imagesToDelete, img)
		}
	}

	// 级联删除变体
	for _, img := range imagesToDelete {
		s.deleteVariantsForImage(ctx, img)
	}

	// 删除数据库记录
	affectedCount, err := s.repo.DeleteImagesByIdentifiersAndUser(identifiers, userID)
	if err != nil {
		// 记录错误但继续清理缓存
		log.Printf("Failed to delete image records: %v", err)
	}

	// 清除缓存
	for _, identifier := range identifiers {
		s.clearImageCache(ctx, identifier)
	}

	return &DeleteResult{Success: true, DeletedCount: affectedCount}, nil
}

// deleteVariantsForImage 删除图片的所有变体
func (s *DeleteService) deleteVariantsForImage(ctx context.Context, img *models.Image) {
	// 获取所有变体
	variants, err := s.variantRepo.GetVariantsByImageID(img.ID)
	if err != nil {
		log.Printf("Failed to get variants for image %d: %v", img.ID, err)
		return
	}

	// 获取存储 provider
	provider, err := s.storageFactory.GetByID(img.StorageConfigID)
	if err != nil {
		log.Printf("Failed to get storage provider for image %d: %v", img.ID, err)
		provider = nil
	}

	// 删除每个变体的文件和缓存
	for _, variant := range variants {
		// 跳过未完成的变体
		if variant.Identifier == "" || variant.Status != models.VariantStatusCompleted {
			continue
		}

		// 删除文件
		if provider != nil {
			if err := provider.DeleteWithContext(ctx, variant.Identifier); err != nil {
				log.Printf("Failed to delete variant file %s: %v", variant.Identifier, err)
			}
		}

		// 清除变体缓存
		if err := s.cacheHelper.DeleteCachedImageData(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete cache for variant %s: %v", utils.SanitizeLogMessage(variant.Identifier), err)
		}
	}

	// 删除数据库中的变体记录
	if err := s.variantRepo.DeleteByImageID(img.ID); err != nil {
		log.Printf("Failed to delete variant records for image %d: %v", img.ID, err)
	}
}

// clearImageCache 清除图片缓存
func (s *DeleteService) clearImageCache(ctx context.Context, identifier string) {
	if err := s.cacheHelper.DeleteCachedImage(ctx, identifier); err != nil {
		log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
	if err := s.cacheHelper.DeleteCachedImageData(ctx, identifier); err != nil {
		log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
}

// DeleteImageVariants 删除指定图片的所有变体（公开方法）
func (s *DeleteService) DeleteImageVariants(ctx context.Context, img *models.Image) error {
	s.deleteVariantsForImage(ctx, img)
	return nil
}

// ClearImageCache 清除指定图片的缓存（公开方法）
func (s *DeleteService) ClearImageCache(ctx context.Context, identifier string) error {
	s.clearImageCache(ctx, identifier)
	return nil
}
