package config

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// ImageProcessingSettings 图片处理配置（缩略图、格式转换、用户偏好）
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

	// 用户偏好配置
	DefaultAlbumID        uint   `json:"default_album_id" mapstructure:"default_album_id"`
	DefaultVisibility     string `json:"default_visibility" mapstructure:"default_visibility"`
	ConcurrentUploadLimit int    `json:"concurrent_upload_limit" mapstructure:"concurrent_upload_limit"`
	MaxFileSizeMB         int    `json:"max_file_size_mb" mapstructure:"max_file_size_mb"`
	MaxBatchTotalMB       int    `json:"max_batch_total_mb" mapstructure:"max_batch_total_mb"`
	APIKeyEnabled         bool   `json:"api_key_enabled" mapstructure:"api_key_enabled"`
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

		// 用户偏好默认值
		DefaultAlbumID:        0,
		DefaultVisibility:     "public",
		ConcurrentUploadLimit: 3,
		MaxFileSizeMB:         50,
		MaxBatchTotalMB:       500,
		APIKeyEnabled:         true,
	}
}

// Validate 验证配置有效性（支持部分验证，零值跳过范围检查）
func (s *ImageProcessingSettings) Validate() error {
	// 只在值非零时进行范围验证（支持部分更新场景）
	if s.ThumbnailQuality != 0 && (s.ThumbnailQuality < 1 || s.ThumbnailQuality > 100) {
		return fmt.Errorf("thumbnail quality must be between 1 and 100")
	}
	// 验证缩略图尺寸
	const maxThumbnailSize = 4096
	for i, size := range s.ThumbnailSizes {
		if size.Width < 0 || size.Height < 0 {
			return fmt.Errorf("invalid thumbnail size at index %d: width and height must be non-negative", i)
		}
		if size.Width > maxThumbnailSize || size.Height > maxThumbnailSize {
			return fmt.Errorf("thumbnail size at index %d exceeds maximum allowed (%dx%d)", i, maxThumbnailSize, maxThumbnailSize)
		}
	}
	if s.WebPQuality != 0 && (s.WebPQuality < 1 || s.WebPQuality > 100) {
		return fmt.Errorf("webp quality must be between 1 and 100")
	}
	if s.WebPEffort < 0 || s.WebPEffort > 6 {
		return fmt.Errorf("webp effort must be between 0 and 6")
	}
	if s.AVIFQuality != 0 && (s.AVIFQuality < 1 || s.AVIFQuality > 100) {
		return fmt.Errorf("avif quality must be between 1 and 100")
	}
	if s.AVIFSpeed < 0 || s.AVIFSpeed > 8 {
		return fmt.Errorf("avif speed must be between 0 and 8")
	}
	// 用户偏好验证（非零值才验证）
	if s.ConcurrentUploadLimit != 0 && (s.ConcurrentUploadLimit < 1 || s.ConcurrentUploadLimit > 10) {
		return fmt.Errorf("concurrent upload limit must be between 1 and 10")
	}
	if s.MaxFileSizeMB != 0 && (s.MaxFileSizeMB < 1 || s.MaxFileSizeMB > 500) {
		return fmt.Errorf("max file size must be between 1 and 500 MB")
	}
	if s.MaxBatchTotalMB != 0 && (s.MaxBatchTotalMB < 1 || s.MaxBatchTotalMB > 500) {
		return fmt.Errorf("max batch total size must be between 1 and 500 MB")
	}
	if s.DefaultVisibility != "" && s.DefaultVisibility != "public" && s.DefaultVisibility != "private" {
		return fmt.Errorf("default visibility must be 'public' or 'private'")
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
		Config: map[string]any{
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
			"default_album_id":           defaultSettings.DefaultAlbumID,
			"default_visibility":         defaultSettings.DefaultVisibility,
			"concurrent_upload_limit":    defaultSettings.ConcurrentUploadLimit,
			"max_file_size_mb":           defaultSettings.MaxFileSizeMB,
			"max_batch_total_mb":         defaultSettings.MaxBatchTotalMB,
			"api_key_enabled":            defaultSettings.APIKeyEnabled,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Image processing and user preference configuration",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default image processing config: %w", err)
	}

	configManagerLog.Infof("Default image processing config created successfully")
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
		Config: map[string]any{
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
			"default_album_id":           settings.DefaultAlbumID,
			"default_visibility":         settings.DefaultVisibility,
			"concurrent_upload_limit":    settings.ConcurrentUploadLimit,
			"max_file_size_mb":           settings.MaxFileSizeMB,
			"max_batch_total_mb":         settings.MaxBatchTotalMB,
			"api_key_enabled":            settings.APIKeyEnabled,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Image processing and user preference configuration",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}

// InitializeImageProcessingConfig 初始化图片处理配置
func (m *Manager) InitializeImageProcessingConfig(ctx context.Context) error {
	return m.ensureDefaultImageProcessingConfig(ctx)
}

// MaskSensitiveData 脱敏敏感数据
func MaskSensitiveData(config map[string]any) map[string]any {
	sensitiveFields := []string{
		"secret", "secret_access_key", "access_key_id", "password",
	}

	result := make(map[string]any)
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
func getStringFromMap(m map[string]any, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}
