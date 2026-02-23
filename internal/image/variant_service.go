package image

import (
	"context"
	"fmt"
	"strings"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
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
	// 读取配置
	settings, err := s.configManager.GetConversionSettings(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[VariantNegotiation] image=%s, variantStatus=%d, acceptHeader=%s\n", image.Identifier, uint(image.VariantStatus), acceptHeader)
	fmt.Printf("[VariantNegotiation] enabledFormats=%v\n", settings.EnabledFormats)

	switch image.VariantStatus {
	case models.ImageVariantStatusNone:
		// 从未处理过，触发转换并返回原图
		return s.handleOriginalWithConversion(image, acceptHeader, settings, true)
	case models.ImageVariantStatusProcessing:
		// 正在处理中，返回原图
		return s.handleOriginalWithConversion(image, acceptHeader, settings, false)
	case models.ImageVariantStatusFailed:
		// 处理失败，可触发重试
		return s.handleOriginalWithConversion(image, acceptHeader, settings, true)
	case models.ImageVariantStatusThumbnailCompleted, models.ImageVariantStatusCompleted:
		// 缩略图或全部已完成，查询变体表
		return s.handleCompletedVariants(ctx, image, acceptHeader, settings)
	default:
		// 默认按 None 处理
		return s.handleOriginalWithConversion(image, acceptHeader, settings, false)
	}
}

// handleOriginalWithConversion 返回原图，根据条件触发转换
func (s *VariantService) handleOriginalWithConversion(image *models.Image, acceptHeader string, settings *config.ConversionSettings, allowTrigger bool) (*VariantResult, error) {
	result := &VariantResult{
		Format:      format.FormatOriginal,
		IsOriginal:  true,
		Image:       image,
		MIMEType:    image.MimeType,
		Identifier:  image.Identifier,
		StoragePath: image.StoragePath,
	}

	// 检查是否需要触发 WebP 转换
	if allowTrigger && strings.Contains(acceptHeader, "image/webp") {
		go s.converter.TriggerWebPConversion(image)
	}

	return result, nil
}

// handleCompletedVariants 处理已完成变体的情况
func (s *VariantService) handleCompletedVariants(ctx context.Context, image *models.Image, acceptHeader string, settings *config.ConversionSettings) (*VariantResult, error) {
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

	// 调试日志
	fmt.Printf("[VariantNegotiation] image=%s, variantStatus=%d, acceptHeader=%s\n", image.Identifier, uint(image.VariantStatus), acceptHeader)
	fmt.Printf("[VariantNegotiation] availableVariants=%v, enabledFormats=%v\n", available, settings.EnabledFormats)

	// 格式协商
	negotiator := format.NewNegotiator(settings.EnabledFormats)
	selectedFormat := negotiator.Negotiate(acceptHeader, available)

	fmt.Printf("[VariantNegotiation] selectedFormat=%s\n", selectedFormat)

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
		} else {
			// 变体不存在（异常情况），降级返回原图
			result.Identifier = image.Identifier
			result.StoragePath = image.StoragePath
			result.IsOriginal = true
			result.MIMEType = image.MimeType
		}
	}

	return result, nil
}
