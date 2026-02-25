package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// ImageProcessingSettings 图片处理配置（合并缩略图、格式转换、扫描器）
type ImageProcessingSettings struct {
	// 缩略图配置
	ThumbnailEnabled bool                   `json:"thumbnail_enabled" mapstructure:"thumbnail_enabled"`
	ThumbnailSizes   []models.ThumbnailSize `json:"thumbnail_sizes" mapstructure:"thumbnail_sizes"`
	ThumbnailQuality int                    `json:"thumbnail_quality" mapstructure:"thumbnail_quality"`

	// 格式转换配置
	ConversionEnabledFormats []string `json:"conversion_enabled_formats" mapstructure:"conversion_enabled_formats"`
	WebPQuality              int      `json:"webp_quality" mapstructure:"webp_quality"`
	WebPEffort               int      `json:"webp_effort" mapstructure:"webp_effort"`
	AVIFQuality              int      `json:"avif_quality" mapstructure:"avif_quality"`
	AVIFSpeed                int      `json:"avif_speed" mapstructure:"avif_speed"`
	AVIFExperimental         bool     `json:"avif_experimental" mapstructure:"avif_experimental"`
	SkipSmallerThan          int      `json:"skip_smaller_than" mapstructure:"skip_smaller_than"`
	MaxDimension             int      `json:"max_dimension" mapstructure:"max_dimension"`
	MaxRetries               int      `json:"max_retries" mapstructure:"max_retries"`

	// 扫描器配置
	ScannerEnabled       bool          `json:"scanner_enabled" mapstructure:"scanner_enabled"`
	ScannerInterval      time.Duration `json:"scanner_interval" mapstructure:"scanner_interval"`
	ScannerBatchSize     int           `json:"scanner_batch_size" mapstructure:"scanner_batch_size"`
	ScannerMaxFileSizeMB int           `json:"scanner_max_file_size_mb" mapstructure:"scanner_max_file_size_mb"`
	ScannerMaxAgeDays    int           `json:"scanner_max_age_days" mapstructure:"scanner_max_age_days"`
	ScannerOnlyPublic    bool          `json:"scanner_only_public" mapstructure:"scanner_only_public"`
}

// DefaultImageProcessingSettings 默认图片处理配置
func DefaultImageProcessingSettings() *ImageProcessingSettings {
	return &ImageProcessingSettings{
		// 缩略图默认值
		ThumbnailEnabled: true,
		ThumbnailSizes:   models.DefaultThumbnailSizes,
		ThumbnailQuality: 85,

		// 格式转换默认值
		ConversionEnabledFormats: []string{"webp"},
		WebPQuality:              75,
		WebPEffort:               4,
		AVIFQuality:              80,
		AVIFSpeed:                4,
		AVIFExperimental:         false,
		SkipSmallerThan:          10,
		MaxDimension:             4096,
		MaxRetries:               3,

		// 扫描器默认值
		ScannerEnabled:       true,
		ScannerInterval:      2 * time.Hour,
		ScannerBatchSize:     50,
		ScannerMaxFileSizeMB: 100,
		ScannerMaxAgeDays:    30,
		ScannerOnlyPublic:    false,
	}
}

// Validate 验证配置有效性
func (s *ImageProcessingSettings) Validate() error {
	if s.ScannerInterval < time.Minute {
		return fmt.Errorf("scan interval must be at least 1 minute")
	}
	if s.ScannerBatchSize < 1 || s.ScannerBatchSize > 1000 {
		return fmt.Errorf("batch size must be between 1 and 1000")
	}
	if s.ScannerMaxFileSizeMB < 0 {
		return fmt.Errorf("max file size cannot be negative")
	}
	if s.ScannerMaxAgeDays < 0 {
		return fmt.Errorf("max age days cannot be negative")
	}
	if s.ThumbnailQuality < 1 || s.ThumbnailQuality > 100 {
		return fmt.Errorf("thumbnail quality must be between 1 and 100")
	}
	if s.WebPQuality < 1 || s.WebPQuality > 100 {
		return fmt.Errorf("webp quality must be between 1 and 100")
	}
	if s.AVIFQuality < 1 || s.AVIFQuality > 100 {
		return fmt.Errorf("avif quality must be between 1 and 100")
	}
	return nil
}

// IsThumbnailEnabled 检查缩略图是否启用
func (s *ImageProcessingSettings) IsThumbnailEnabled() bool {
	return s.ThumbnailEnabled
}

// IsValidWidth 检查是否为有效的缩略图宽度
func (s *ImageProcessingSettings) IsValidWidth(width int) bool {
	for _, size := range s.ThumbnailSizes {
		if size.Width == width {
			return true
		}
	}
	return false
}

// GetSizeByWidth 根据宽度获取尺寸配置
func (s *ImageProcessingSettings) GetSizeByWidth(width int) *models.ThumbnailSize {
	for _, size := range s.ThumbnailSizes {
		if size.Width == width {
			return &size
		}
	}
	return nil
}

// IsFormatEnabled 检查格式是否启用
func (s *ImageProcessingSettings) IsFormatEnabled(format string) bool {
	for _, f := range s.ConversionEnabledFormats {
		if f == format {
			return true
		}
	}
	return false
}

// GetImageProcessingSettings 获取图片处理配置
func (m *Manager) GetImageProcessingSettings(ctx context.Context) (*ImageProcessingSettings, error) {
	// 先从缓存获取
	if cached := m.cache.GetImageProcessing(); cached != nil {
		return cached, nil
	}

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryImageProcessing)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := m.ensureDefaultImageProcessingConfig(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default image processing config: %w", err)
			}
			config, err = m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryImageProcessing)
			if err != nil {
				return nil, fmt.Errorf("failed to get image processing config after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get image processing config: %w", err)
		}
	}

	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt image processing config: %w", err)
	}

	settings := &ImageProcessingSettings{}
	if err := mapstructure.Decode(configMap, settings); err != nil {
		return nil, fmt.Errorf("failed to decode image processing settings: %w", err)
	}

	m.cache.SetImageProcessing(settings)
	return settings, nil
}

// ensureDefaultImageProcessingConfig 确保默认图片处理配置存在
func (m *Manager) ensureDefaultImageProcessingConfig(ctx context.Context) error {
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryImageProcessing)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	defaultSettings := DefaultImageProcessingSettings()

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryImageProcessing,
		Name:     "Image Processing Settings",
		Config: map[string]interface{}{
			"thumbnail_enabled":          defaultSettings.ThumbnailEnabled,
			"thumbnail_sizes":            defaultSettings.ThumbnailSizes,
			"thumbnail_quality":          defaultSettings.ThumbnailQuality,
			"conversion_enabled_formats": defaultSettings.ConversionEnabledFormats,
			"webp_quality":               defaultSettings.WebPQuality,
			"webp_effort":                defaultSettings.WebPEffort,
			"avif_quality":               defaultSettings.AVIFQuality,
			"avif_speed":                 defaultSettings.AVIFSpeed,
			"avif_experimental":          defaultSettings.AVIFExperimental,
			"skip_smaller_than":          defaultSettings.SkipSmallerThan,
			"max_dimension":              defaultSettings.MaxDimension,
			"max_retries":                defaultSettings.MaxRetries,
			"scanner_enabled":            defaultSettings.ScannerEnabled,
			"scanner_interval":           defaultSettings.ScannerInterval,
			"scanner_batch_size":         defaultSettings.ScannerBatchSize,
			"scanner_max_file_size_mb":   defaultSettings.ScannerMaxFileSizeMB,
			"scanner_max_age_days":       defaultSettings.ScannerMaxAgeDays,
			"scanner_only_public":        defaultSettings.ScannerOnlyPublic,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Image processing configuration (thumbnail, conversion, scanner)",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default image processing config: %w", err)
	}

	log.Println("[ConfigManager] Default image processing config created successfully")
	return nil
}

// SaveImageProcessingSettings 保存图片处理配置
func (m *Manager) SaveImageProcessingSettings(ctx context.Context, settings *ImageProcessingSettings, userID uint) error {
	if err := settings.Validate(); err != nil {
		return err
	}

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryImageProcessing)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m.ensureDefaultImageProcessingConfig(ctx)
		}
		return err
	}

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryImageProcessing,
		Name:     "Image Processing Settings",
		Config: map[string]interface{}{
			"thumbnail_enabled":          settings.ThumbnailEnabled,
			"thumbnail_sizes":            settings.ThumbnailSizes,
			"thumbnail_quality":          settings.ThumbnailQuality,
			"conversion_enabled_formats": settings.ConversionEnabledFormats,
			"webp_quality":               settings.WebPQuality,
			"webp_effort":                settings.WebPEffort,
			"avif_quality":               settings.AVIFQuality,
			"avif_speed":                 settings.AVIFSpeed,
			"avif_experimental":          settings.AVIFExperimental,
			"skip_smaller_than":          settings.SkipSmallerThan,
			"max_dimension":              settings.MaxDimension,
			"max_retries":                settings.MaxRetries,
			"scanner_enabled":            settings.ScannerEnabled,
			"scanner_interval":           settings.ScannerInterval,
			"scanner_batch_size":         settings.ScannerBatchSize,
			"scanner_max_file_size_mb":   settings.ScannerMaxFileSizeMB,
			"scanner_max_age_days":       settings.ScannerMaxAgeDays,
			"scanner_only_public":        settings.ScannerOnlyPublic,
		},
		IsEnabled:   BoolPtr(settings.ThumbnailEnabled),
		IsDefault:   BoolPtr(true),
		Description: "Image processing configuration (thumbnail, conversion, scanner)",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}

// InitializeImageProcessingConfig 初始化图片处理配置
func (m *Manager) InitializeImageProcessingConfig(ctx context.Context) error {
	return m.ensureDefaultImageProcessingConfig(ctx)
}

// MaskSensitiveData 脱敏敏感数据
func MaskSensitiveData(config map[string]interface{}) map[string]interface{} {
	sensitiveFields := []string{
		"secret", "secret_access_key", "access_key_id", "password",
	}

	result := make(map[string]interface{})
	for k, v := range config {
		isSensitive := false
		for _, sf := range sensitiveFields {
			if strings.EqualFold(k, sf) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			result[k] = "******"
		} else {
			result[k] = v
		}
	}

	return result
}

// BoolPtr 返回 bool 指针
func BoolPtr(b bool) *bool {
	return &b
}

// getStringFromMap 从 map 中获取字符串值，提供默认值
func getStringFromMap(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}
