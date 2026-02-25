package image

import (
	"context"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/format"
)

// VariantResult 变体选择结果
type VariantResult struct {
	Format      format.FormatType
	IsOriginal  bool
	Image       *models.Image
	Variant     *models.ImageVariant
	MIMEType    string
	Identifier  string
	StoragePath string
}

// VariantService 变体服务
type VariantService struct {
	variantRepo   *images.VariantRepository
	configManager *config.Manager
	converter     *Converter
}

// NewVariantService 创建服务
func NewVariantService(repo *images.VariantRepository, cm *config.Manager, converter *Converter) *VariantService {
	return &VariantService{
		variantRepo:   repo,
		configManager: cm,
		converter:     converter,
	}
}

// SelectBestVariant 选择最优格式变体
func (s *VariantService) SelectBestVariant(ctx context.Context, image *models.Image, acceptHeader string) (*VariantResult, error) {
	// GIF 和 WebP 格式直接返回原图，不进行格式协商
	if image.MimeType == "image/gif" || image.MimeType == "image/webp" {
		return &VariantResult{
			Format:      format.FormatOriginal,
			IsOriginal:  true,
			Image:       image,
			MIMEType:    image.MimeType,
			Identifier:  image.Identifier,
			StoragePath: image.StoragePath,
		}, nil
	}

	settings, err := s.configManager.GetImageProcessingSettings(ctx)
	if err != nil {
		return nil, err
	}

	// utils.LogIfDevf("[VariantNegotiation] image=%s, variantStatus=%d, acceptHeader=%s", image.Identifier, uint(image.VariantStatus), acceptHeader)
	// utils.LogIfDevf("[VariantNegotiation] enabledFormats=%v", settings.ConversionEnabledFormats)

	switch image.VariantStatus {
	case models.ImageVariantStatusNone:
		// 从未处理过，触发转换并返回原图
		return s.handleOriginalWithConversion(image, acceptHeader, settings, true)
	case models.ImageVariantStatusProcessing:
		// 正在处理中，返回原图
		return s.handleOriginalWithConversion(image, acceptHeader, settings, false)
	case models.ImageVariantStatusFailed:
		return s.handleOriginalWithConversion(image, acceptHeader, settings, true)
	case models.ImageVariantStatusThumbnailCompleted, models.ImageVariantStatusCompleted:

		return s.handleCompletedVariants(ctx, image, acceptHeader, settings)
	default:
		// 默认按 None 处理
		return s.handleOriginalWithConversion(image, acceptHeader, settings, false)
	}
}

// handleOriginalWithConversion 返回原图，根据条件触发转换
func (s *VariantService) handleOriginalWithConversion(image *models.Image, acceptHeader string, settings *config.ImageProcessingSettings, allowTrigger bool) (*VariantResult, error) {
	result := &VariantResult{
		Format:      format.FormatOriginal,
		IsOriginal:  true,
		Image:       image,
		MIMEType:    image.MimeType,
		Identifier:  image.Identifier,
		StoragePath: image.StoragePath,
	}

	_ = allowTrigger
	_ = acceptHeader

	return result, nil
}

// handleCompletedVariants 处理已完成变体的情况
func (s *VariantService) handleCompletedVariants(ctx context.Context, image *models.Image, acceptHeader string, settings *config.ImageProcessingSettings) (*VariantResult, error) {
	variants, err := s.variantRepo.GetVariantsByImageID(image.ID)
	if err != nil {
		return nil, err
	}

	available := make(map[format.FormatType]bool)
	variantMap := make(map[format.FormatType]*models.ImageVariant)

	for _, v := range variants {
		if v.Status == models.VariantStatusCompleted {
			ft := format.FormatType(v.Format)
			available[ft] = true
			variantMap[ft] = &v
		}
	}

	// 调试日志（仅在 dev 环境显示）
	utils.LogIfDevf("[VariantNegotiation] image=%s, variantStatus=%d, acceptHeader=%s", image.Identifier, uint(image.VariantStatus), acceptHeader)
	utils.LogIfDevf("[VariantNegotiation] availableVariants=%v, enabledFormats=%v", available, settings.ConversionEnabledFormats)

	negotiator := format.NewNegotiator(settings.ConversionEnabledFormats)
	selectedFormat := negotiator.Negotiate(acceptHeader, available)

	utils.LogIfDevf("[VariantNegotiation] selectedFormat=%s", selectedFormat)

	result := &VariantResult{
		Format: selectedFormat,
		Image:  image,
	}

	if selectedFormat == format.FormatOriginal {
		result.IsOriginal = true
		result.MIMEType = image.MimeType
		result.Identifier = image.Identifier
		result.StoragePath = image.StoragePath
	} else {
		variant := variantMap[selectedFormat]
		result.Variant = variant
		result.MIMEType = format.FormatRegistry[selectedFormat].MIMEType

		if variant != nil {
			result.Identifier = variant.Identifier
			result.StoragePath = variant.StoragePath
		}
	}

	return result, nil
}
