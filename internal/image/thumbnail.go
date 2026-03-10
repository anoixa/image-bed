package image

import (
	"context"
	"errors"
	"fmt"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils/generator"
	"gorm.io/gorm"
)

// ThumbnailResult 缩略图结果
type ThumbnailResult struct {
	Format      string
	Identifier  string
	StoragePath string
	Width       int
	Height      int
	FileSize    int64
	MIMEType    string
}

// ThumbnailService 缩略图服务
type ThumbnailService struct {
	variantRepo   *images.VariantRepository
	imageRepo     *images.Repository
	configManager *config.Manager
	storage       storage.Provider
	converter     *Converter
	pathGenerator *generator.PathGenerator
}

// NewThumbnailService 创建缩略图服务
func NewThumbnailService(
	variantRepo *images.VariantRepository,
	imageRepo *images.Repository,
	cm *config.Manager,
	storage storage.Provider,
	converter *Converter,
) *ThumbnailService {
	return &ThumbnailService{
		variantRepo:   variantRepo,
		imageRepo:     imageRepo,
		configManager: cm,
		storage:       storage,
		converter:     converter,
		pathGenerator: generator.NewPathGenerator(),
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
		Width:       variant.Width,
		Height:      variant.Height,
		FileSize:    variant.FileSize,
		MIMEType:    s.getMIMETypeFromFormat(format),
	}, nil
}

// TriggerGeneration 触发缩略图生成
func (s *ThumbnailService) TriggerGeneration(image *models.Image, width int) {
	// 缩略图生成已完全迁移至 ImagePipelineTask
}

// TriggerGenerationForAllSizes 为所有配置尺寸生成缩略图
func (s *ThumbnailService) TriggerGenerationForAllSizes(image *models.Image) {
	// 缩略图生成已完全迁移至 ImagePipelineTask
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

// GenerateThumbnailIdentifiers 生成缩略图的 identifier 和 storage_path
func (s *ThumbnailService) GenerateThumbnailIdentifiers(originalStoragePath string, width int) generator.StorageIdentifiers {
	return s.pathGenerator.GenerateThumbnailIdentifiers(originalStoragePath, width)
}

// getMIMETypeFromFormat 根据格式获取 MIME 类型
func (s *ThumbnailService) getMIMETypeFromFormat(format string) string {
	return "image/webp"
}

// GetThumbnailURL 获取缩略图 URL
func (s *ThumbnailService) GetThumbnailURL(originalStoragePath string, width int) string {
	ids := s.GenerateThumbnailIdentifiers(originalStoragePath, width)
	return ids.StoragePath
}

// formatThumbnailSize 生成缩略图格式标识
func formatThumbnailSize(width int) string {
	return fmt.Sprintf("thumbnail_%d", width)
}
