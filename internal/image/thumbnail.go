package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"gorm.io/gorm"
)

// ThumbnailResult 缩略图结果
type ThumbnailResult struct {
	Format      string
	Identifier  string
	StoragePath string
	FileHash    string
	Width       int
	Height      int
	FileSize    int64
	MIMEType    string
}

// ThumbnailService 缩略图服务
type ThumbnailService struct {
	variantRepo *images.VariantRepository
}

// NewThumbnailService 创建缩略图服务
func NewThumbnailService(variantRepo *images.VariantRepository) *ThumbnailService {
	return &ThumbnailService{
		variantRepo: variantRepo,
	}
}

// GetThumbnail 获取缩略图信息
func (s *ThumbnailService) GetThumbnail(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, error) {
	format := formatThumbnailSize(width)

	variant, err := s.variantRepo.GetVariantByImageIDAndFormat(image.ID, format)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get thumbnail variant: %w", err)
	}

	if variant.Status != models.VariantStatusCompleted {
		return nil, nil
	}

	return &ThumbnailResult{
		Format:      format,
		Identifier:  variant.Identifier,
		StoragePath: variant.StoragePath,
		FileHash:    variant.FileHash,
		Width:       variant.Width,
		Height:      variant.Height,
		FileSize:    variant.FileSize,
		MIMEType:    "image/webp",
	}, nil
}

// EnsureThumbnail 确保缩略图存在
func (s *ThumbnailService) EnsureThumbnail(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, bool, error) {
	result, err := s.GetThumbnail(ctx, image, width)
	if err != nil {
		return nil, false, err
	}

	if result != nil {
		return result, true, nil
	}

	return nil, false, nil
}

// GetWebPVariant 获取 WebP 格式变体（用于缩略图降级）
func (s *ThumbnailService) GetWebPVariant(ctx context.Context, image *models.Image) (*ThumbnailResult, bool, error) {
	variant, err := s.variantRepo.GetVariantByImageIDAndFormat(image.ID, "webp")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if variant.Status != models.VariantStatusCompleted {
		return nil, false, nil
	}

	return &ThumbnailResult{
		Format:      "webp",
		Identifier:  variant.Identifier,
		StoragePath: variant.StoragePath,
		FileHash:    variant.FileHash,
		Width:       variant.Width,
		Height:      variant.Height,
		FileSize:    variant.FileSize,
		MIMEType:    "image/webp",
	}, true, nil
}

// formatThumbnailSize 生成缩略图格式标识
func formatThumbnailSize(width int) string {
	return fmt.Sprintf("thumbnail_%d", width)
}
