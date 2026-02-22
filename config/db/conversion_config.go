package config

import (
	"context"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// ConversionSettings 格式转换配置
type ConversionSettings struct {
	EnabledFormats   []string `json:"enabled_formats" mapstructure:"enabled_formats"`     // ["webp"]
	WebPQuality      int      `json:"webp_quality" mapstructure:"webp_quality"`           // 1-100, 默认 85
	WebPEffort       int      `json:"webp_effort" mapstructure:"webp_effort"`             // 0-6, 默认 4
	AVIFQuality      int      `json:"avif_quality" mapstructure:"avif_quality"`           // 1-100, 默认 80
	AVIFSpeed        int      `json:"avif_speed" mapstructure:"avif_speed"`               // 0-10, 默认 4
	AVIFExperimental bool     `json:"avif_experimental" mapstructure:"avif_experimental"` // 实验性标记
	SkipSmallerThan  int      `json:"skip_smaller_than" mapstructure:"skip_smaller_than"` // KB, 默认 10
	MaxDimension     int      `json:"max_dimension" mapstructure:"max_dimension"`         // 默认 4096
	MaxRetries       int      `json:"max_retries" mapstructure:"max_retries"`             // 默认 3
}

// DefaultConversionSettings 默认配置
func DefaultConversionSettings() *ConversionSettings {
	return &ConversionSettings{
		EnabledFormats:   []string{},
		WebPQuality:      85,
		WebPEffort:       4,
		AVIFQuality:      80,
		AVIFSpeed:        4,
		AVIFExperimental: false,
		SkipSmallerThan:  10,
		MaxDimension:     4096,
		MaxRetries:       3,
	}
}

// IsFormatEnabled 检查格式是否启用
func (s *ConversionSettings) IsFormatEnabled(format string) bool {
	for _, f := range s.EnabledFormats {
		if f == format {
			return true
		}
	}
	return false
}

// GetConversionSettings 获取转换配置
func (m *Manager) GetConversionSettings(ctx context.Context) (*ConversionSettings, error) {
	m.cacheMutex.RLock()
	if val, exists := m.localCache[cacheKeyConversion]; exists {
		m.cacheMutex.RUnlock()
		return val.(*ConversionSettings), nil
	}
	m.cacheMutex.RUnlock()

	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// 双重检查
	if val, exists := m.localCache[cacheKeyConversion]; exists {
		return val.(*ConversionSettings), nil
	}

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryConversion)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := m.EnsureDefaultConversionConfig(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default conversion config: %w", err)
			}
			// 重新获取
			config, err = m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryConversion)
			if err != nil {
				return nil, fmt.Errorf("failed to get conversion config after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get conversion config: %w", err)
		}
	}

	// 解密配置
	configMap, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt conversion config: %w", err)
	}

	settings := &ConversionSettings{}
	if err := mapstructure.Decode(configMap, settings); err != nil {
		return nil, fmt.Errorf("failed to decode conversion settings: %w", err)
	}

	// 写入缓存
	m.localCache[cacheKeyConversion] = settings

	return settings, nil
}

// EnsureDefaultConversionConfig 确保默认转换配置存在
func (m *Manager) EnsureDefaultConversionConfig(ctx context.Context) error {
	// 检查是否已存在
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryConversion)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	defaultSettings := DefaultConversionSettings()

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryConversion,
		Name:     "格式转换配置",
		Config: map[string]interface{}{
			"enabled_formats":   defaultSettings.EnabledFormats,
			"webp_quality":      defaultSettings.WebPQuality,
			"webp_effort":       defaultSettings.WebPEffort,
			"avif_quality":      defaultSettings.AVIFQuality,
			"avif_speed":        defaultSettings.AVIFSpeed,
			"avif_experimental": defaultSettings.AVIFExperimental,
			"skip_smaller_than": defaultSettings.SkipSmallerThan,
			"max_dimension":     defaultSettings.MaxDimension,
			"max_retries":       defaultSettings.MaxRetries,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Image Format Conversion Settings（WebP/AVIF）",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default conversion config: %w", err)
	}

	log.Println("[ConfigManager] Default conversion config created successfully")
	return nil
}

// SaveConversionSettings 保存转换配置
func (m *Manager) SaveConversionSettings(ctx context.Context, settings *ConversionSettings, userID uint) error {
	// 获取现有配置
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryConversion)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新配置
			return m.EnsureDefaultConversionConfig(ctx)
		}
		return err
	}

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryConversion,
		Name:     "格式转换配置",
		Config: map[string]interface{}{
			"enabled_formats":   settings.EnabledFormats,
			"webp_quality":      settings.WebPQuality,
			"webp_effort":       settings.WebPEffort,
			"avif_quality":      settings.AVIFQuality,
			"avif_speed":        settings.AVIFSpeed,
			"avif_experimental": settings.AVIFExperimental,
			"skip_smaller_than": settings.SkipSmallerThan,
			"max_dimension":     settings.MaxDimension,
			"max_retries":       settings.MaxRetries,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "图片格式转换设置（WebP/AVIF）",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}

// InitializeConversionConfig 初始化转换配置
func (m *Manager) InitializeConversionConfig(ctx context.Context) error {
	return m.EnsureDefaultConversionConfig(ctx)
}
