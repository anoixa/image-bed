package config

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// ThumbnailScannerConfig 缩略图扫描器配置
type ThumbnailScannerConfig struct {
	Enabled          bool          `json:"enabled" mapstructure:"enabled"`                       // 是否启用扫描器
	Interval         time.Duration `json:"interval" mapstructure:"interval"`                     // 扫描间隔
	BatchSize        int           `json:"batch_size" mapstructure:"batch_size"`                 // 每批处理数量
	MaxFileSizeMB    int           `json:"max_file_size_mb" mapstructure:"max_file_size_mb"`     // 最大文件大小(MB)
	MaxAgeDays       int           `json:"max_age_days" mapstructure:"max_age_days"`             // 最大图片年龄(天)
	OnlyPublicImages bool          `json:"only_public_images" mapstructure:"only_public_images"` // 仅处理公开图片
}

// Validate 验证配置有效性
func (c *ThumbnailScannerConfig) Validate() error {
	if c.Interval < time.Minute {
		return fmt.Errorf("scan interval must be at least 1 minute")
	}
	if c.BatchSize < 1 {
		return fmt.Errorf("batch size must be at least 1")
	}
	if c.BatchSize > 1000 {
		return fmt.Errorf("batch size cannot exceed 1000")
	}
	if c.MaxFileSizeMB < 0 {
		return fmt.Errorf("max file size cannot be negative")
	}
	if c.MaxAgeDays < 0 {
		return fmt.Errorf("max age days cannot be negative")
	}
	return nil
}

// GetDefaultThumbnailScannerSettings 获取默认缩略图扫描器配置
func GetDefaultThumbnailScannerSettings() *ThumbnailScannerConfig {
	return &ThumbnailScannerConfig{
		Enabled:          true,
		Interval:         2 * time.Hour,
		BatchSize:        50,  // 每批处理50张图片
		MaxFileSizeMB:    100, // 默认只处理小于100MB的图片
		MaxAgeDays:       30,
		OnlyPublicImages: false,
	}
}

// GetThumbnailScannerSettings 获取缩略图扫描器配置
func (m *Manager) GetThumbnailScannerSettings() (*ThumbnailScannerConfig, error) {
	ctx := context.Background()

	m.cacheMutex.RLock()
	if val, exists := m.localCache[cacheKeyThumbnailScanner]; exists {
		m.cacheMutex.RUnlock()
		return val.(*ThumbnailScannerConfig), nil
	}
	m.cacheMutex.RUnlock()

	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// 双重检查
	if val, exists := m.localCache[cacheKeyThumbnailScanner]; exists {
		return val.(*ThumbnailScannerConfig), nil
	}

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryThumbnailScanner)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 不存在，返回默认配置
			settings := GetDefaultThumbnailScannerSettings()
			m.localCache[cacheKeyThumbnailScanner] = settings
			return settings, nil
		}
		return nil, fmt.Errorf("failed to get thumbnail scanner config: %w", err)
	}

	configMap, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		settings := GetDefaultThumbnailScannerSettings()
		m.localCache[cacheKeyThumbnailScanner] = settings
		return settings, nil
	}

	settings := &ThumbnailScannerConfig{}
	if err := mapstructure.Decode(configMap, settings); err != nil {
		settings := GetDefaultThumbnailScannerSettings()
		m.localCache[cacheKeyThumbnailScanner] = settings
		return settings, nil
	}

	m.localCache[cacheKeyThumbnailScanner] = settings

	return settings, nil
}

// SaveThumbnailScannerSettings 保存缩略图扫描器配置
func (m *Manager) SaveThumbnailScannerSettings(settings *ThumbnailScannerConfig) error {
	if err := settings.Validate(); err != nil {
		return err
	}

	ctx := context.Background()

	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryThumbnailScanner)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m.createDefaultThumbnailScannerConfig(settings)
		}
		return err
	}

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryThumbnailScanner,
		Name:     "Thumbnail Scanner Configuration",
		Config: map[string]interface{}{
			"enabled":            settings.Enabled,
			"interval":           settings.Interval,
			"batch_size":         settings.BatchSize,
			"max_file_size_mb":   settings.MaxFileSizeMB,
			"max_age_days":       settings.MaxAgeDays,
			"only_public_images": settings.OnlyPublicImages,
		},
		IsEnabled:   BoolPtr(settings.Enabled),
		IsDefault:   BoolPtr(true),
		Description: "Thumbnail scanner settings for batch generation",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}

// createDefaultThumbnailScannerConfig 创建默认缩略图扫描器配置
func (m *Manager) createDefaultThumbnailScannerConfig(settings *ThumbnailScannerConfig) error {
	ctx := context.Background()

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryThumbnailScanner,
		Name:     "Thumbnail Scanner Configuration",
		Config: map[string]interface{}{
			"enabled":            settings.Enabled,
			"interval":           settings.Interval,
			"batch_size":         settings.BatchSize,
			"max_file_size_mb":   settings.MaxFileSizeMB,
			"max_age_days":       settings.MaxAgeDays,
			"only_public_images": settings.OnlyPublicImages,
		},
		IsEnabled:   BoolPtr(settings.Enabled),
		IsDefault:   BoolPtr(true),
		Description: "Thumbnail scanner settings for batch generation",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	return err
}
