package image

import (
	"context"
	"strings"

	"github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils/format"
)

// VariantResult 变体选择结果
type VariantResult struct {
	Format     format.FormatType
	IsOriginal bool
	Image      *models.Image
	Variant    *models.ImageVariant
	MIMEType   string
	Identifier string
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
	// 读取配置
	settings, err := s.configManager.GetConversionSettings(ctx)
	if err != nil {
		return nil, err
	}

	// 查询可用变体
	variants, err := s.variantRepo.GetVariantsByImageID(image.ID)
	if err != nil {
		return nil, err
	}

	// 构建可用格式映射
	available := make(map[format.FormatType]bool)
	variantMap := make(map[format.FormatType]*models.ImageVariant)

	for _, v := range variants {
		if v.Status == models.VariantStatusCompleted {
			ft := format.FormatType(v.Format)
			available[ft] = true
			variantMap[ft] = &v
		}
	}

	// 格式协商
	negotiator := format.NewNegotiator(settings.EnabledFormats)
	selectedFormat := negotiator.Negotiate(acceptHeader, available)

	result := &VariantResult{
		Format: selectedFormat,
		Image:  image,
	}

	if selectedFormat == format.FormatOriginal {
		result.IsOriginal = true
		result.MIMEType = image.MimeType
		result.Identifier = image.Identifier

		if !available[format.FormatWebP] && strings.Contains(acceptHeader, "image/webp") {
			go s.converter.TriggerWebPConversion(image)
		}
	} else {
		variant := variantMap[selectedFormat]
		result.Variant = variant
		result.MIMEType = format.FormatRegistry[selectedFormat].MIMEType

		if variant != nil {
			result.Identifier = variant.Identifier
		} else {
			// 变体不存在，触发后台转换
			result.Identifier = image.Identifier
			go s.converter.TriggerWebPConversion(image)
		}
	}

	return result, nil
}
