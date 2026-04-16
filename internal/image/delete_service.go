package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

var deleteLog = utils.ForModule("Delete")

// DeleteService 负责图片删除与清理相关用例
type DeleteService struct {
	repo        *images.Repository
	variantRepo *images.VariantRepository
	cacheHelper *cache.Helper
}

func NewDeleteService(repo *images.Repository, variantRepo *images.VariantRepository, cacheHelper *cache.Helper) *DeleteService {
	return &DeleteService{
		repo:        repo,
		variantRepo: variantRepo,
		cacheHelper: cacheHelper,
	}
}

func (s *DeleteService) DeleteSingle(ctx context.Context, identifier string, userID uint) (*DeleteResult, error) {
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

	result, imagesToDelete, err := s.repo.DeleteBatchTransaction(ctx, []string{identifier}, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete image: %w", err)
	}
	if result.DeletedCount == 0 || len(imagesToDelete) == 0 {
		return &DeleteResult{Success: false, Error: errors.New("image not found")}, nil
	}

	img = imagesToDelete[0]
	s.deleteVariantsForImage(ctx, img)

	if img.StoragePath != "" {
		refCount, err := s.repo.CountImagesByStoragePath(img.StoragePath)
		if err != nil {
			deleteLog.Errorf("Failed to count references for storage path %s: %v", img.StoragePath, err)
		} else if refCount == 0 {
			provider, err := getStorageProviderByID(img.StorageConfigID)
			if err != nil {
				deleteLog.Errorf("Failed to get storage provider for image %s: %v", utils.SanitizeLogMessage(img.Identifier), err)
			} else if err := provider.DeleteWithContext(ctx, img.StoragePath); err != nil {
				deleteLog.Errorf("Failed to delete original image file %s: %v", img.StoragePath, err)
			}
		} else {
			deleteLog.Debugf("Skipping physical file deletion for %s, still referenced by %d images", img.StoragePath, refCount)
		}
	}

	s.clearImageCache(ctx, identifier)
	return &DeleteResult{Success: true, DeletedCount: 1}, nil
}

func (s *DeleteService) DeleteBatch(ctx context.Context, identifiers []string, userID uint) (*DeleteResult, error) {
	if len(identifiers) == 0 {
		return &DeleteResult{Success: true, DeletedCount: 0}, nil
	}

	result, imagesToDelete, err := s.repo.DeleteBatchTransaction(ctx, identifiers, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch delete: %w", err)
	}

	for _, img := range imagesToDelete {
		s.deleteVariantsForImage(ctx, img)
	}

	for _, img := range imagesToDelete {
		if img.StoragePath == "" {
			continue
		}

		refCount, err := s.repo.CountImagesByStoragePath(img.StoragePath)
		if err != nil {
			deleteLog.Errorf("Failed to count references for storage path %s: %v", img.StoragePath, err)
			continue
		}

		if refCount == 0 {
			provider, err := getStorageProviderByID(img.StorageConfigID)
			if err != nil {
				deleteLog.Errorf("Failed to get storage provider for image %s: %v", utils.SanitizeLogMessage(img.Identifier), err)
			} else if err := provider.DeleteWithContext(ctx, img.StoragePath); err != nil {
				deleteLog.Errorf("Failed to delete original image file %s: %v", img.StoragePath, err)
			}
		} else {
			deleteLog.Debugf("Skipping physical file deletion for %s, still referenced by %d images", img.StoragePath, refCount)
		}
	}

	for _, identifier := range identifiers {
		s.clearImageCache(ctx, identifier)
	}

	return &DeleteResult{Success: true, DeletedCount: result.DeletedCount}, nil
}

func (s *DeleteService) DeleteImageVariants(ctx context.Context, img *models.Image) error {
	s.deleteVariantsForImage(ctx, img)
	return nil
}

func (s *DeleteService) ClearImageCache(ctx context.Context, identifier string) error {
	s.clearImageCache(ctx, identifier)
	return nil
}

func (s *DeleteService) deleteVariantsForImage(ctx context.Context, img *models.Image) {
	variants, err := s.variantRepo.GetVariantsByImageID(img.ID)
	if err != nil {
		deleteLog.Errorf("Failed to get variants for image %d: %v", img.ID, err)
		return
	}

	provider, err := getStorageProviderByID(img.StorageConfigID)
	if err != nil {
		deleteLog.Errorf("Failed to get storage provider for image %d: %v", img.ID, err)
		return
	}

	for _, variant := range variants {
		if variant.StoragePath == "" {
			continue
		}

		if err := provider.DeleteWithContext(ctx, variant.StoragePath); err != nil {
			deleteLog.Errorf("Failed to delete variant file %s: %v", variant.StoragePath, err)
		}

		if s.cacheHelper != nil {
			if err := s.cacheHelper.DeleteCachedImageData(ctx, variant.Identifier); err != nil {
				deleteLog.Warnf("Failed to delete cache for variant %s: %v", utils.SanitizeLogMessage(variant.Identifier), err)
			}
		}
	}

	if err := s.variantRepo.DeleteByImageID(img.ID); err != nil {
		deleteLog.Errorf("Failed to delete variant records for image %d: %v", img.ID, err)
	}
}

func (s *DeleteService) clearImageCache(ctx context.Context, identifier string) {
	if s.cacheHelper == nil {
		return
	}
	if err := s.cacheHelper.DeleteCachedImage(ctx, identifier); err != nil {
		deleteLog.Warnf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
	if err := s.cacheHelper.DeleteCachedImageData(ctx, identifier); err != nil {
		deleteLog.Errorf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(identifier), err)
	}
}
