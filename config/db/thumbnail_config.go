package config

import (
	"context"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// ThumbnailSettings 缩略图配置
type ThumbnailSettings struct {
	Enabled    bool                   `json:"enabled" mapstructure:"enabled"`         // 是否启用
	Sizes      []models.ThumbnailSize `json:"sizes" mapstructure:"sizes"`             // 尺寸配置
	Quality    int                    `json:"quality" mapstructure:"quality"`         // JPEG质量 1-100
	MaxRetries int                    `json:"max_retries" mapstructure:"max_retries"` // 最大重试次数
}

// DefaultThumbnailSettings 默认缩略图配置
func DefaultThumbnailSettings() *ThumbnailSettings {
	return &ThumbnailSettings{
		Enabled:    true,
		Sizes:      models.DefaultThumbnailSizes,
		Quality:    85,
		MaxRetries: 3,
	}
}

// IsValidWidth 检查是否为有效的缩略图宽度
func (s *ThumbnailSettings) IsValidWidth(width int) bool {
	for _, size := range s.Sizes {
		if size.Width == width {
			return true
		}
	}
	return false
}

// GetSizeByWidth 根据宽度获取尺寸配置
func (s *ThumbnailSettings) GetSizeByWidth(width int) *models.ThumbnailSize {
	for _, size := range s.Sizes {
		if size.Width == width {
			return &size
		}
	}
	return nil
}

// GetThumbnailSettings 获取缩略图配置
func (m *Manager) GetThumbnailSettings(ctx context.Context) (*ThumbnailSettings, error) {
	m.cacheMutex.RLock()
	if val, exists := m.localCache[cacheKeyThumbnail]; exists {
		m.cacheMutex.RUnlock()
		return val.(*ThumbnailSettings), nil
	}
	m.cacheMutex.RUnlock()

	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// 双重检查
	if val, exists := m.localCache[cacheKeyThumbnail]; exists {
		return val.(*ThumbnailSettings), nil
	}

	// 获取默认缩略图配置
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryThumbnail)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建默认配置
			if err := m.ensureDefaultThumbnailConfig(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default thumbnail config: %w", err)
			}

			config, err = m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryThumbnail)
			if err != nil {
				return nil, fmt.Errorf("failed to get thumbnail config after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get thumbnail config: %w", err)
		}
	}

	// 解密配置
	configMap, err := m.DecryptConfig(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt thumbnail config: %w", err)
	}

	settings := &ThumbnailSettings{}
	if err := mapstructure.Decode(configMap, settings); err != nil {
		return nil, fmt.Errorf("failed to decode thumbnail settings: %w", err)
	}

	// 写入缓存
	m.localCache[cacheKeyThumbnail] = settings

	return settings, nil
}

// ensureDefaultThumbnailConfig 确保默认缩略图配置存在
func (m *Manager) ensureDefaultThumbnailConfig(ctx context.Context) error {
	// 检查是否已存在
	count, err := m.repo.CountByCategory(ctx, models.ConfigCategoryThumbnail)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	defaultSettings := DefaultThumbnailSettings()

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryThumbnail,
		Name:     "Thumbnail Configuration",
		Config: map[string]interface{}{
			"enabled":     defaultSettings.Enabled,
			"sizes":       defaultSettings.Sizes,
			"quality":     defaultSettings.Quality,
			"max_retries": defaultSettings.MaxRetries,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Thumbnail generation settings",
	}

	_, err = m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default thumbnail config: %w", err)
	}

	log.Println("[ConfigManager] Default thumbnail config created successfully")
	return nil
}

// SaveThumbnailSettings 保存缩略图配置
func (m *Manager) SaveThumbnailSettings(ctx context.Context, settings *ThumbnailSettings) error {
	// 获取现有配置
	config, err := m.repo.GetDefaultByCategory(ctx, models.ConfigCategoryThumbnail)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新配置
			return m.ensureDefaultThumbnailConfig(ctx)
		}
		return err
	}

	// 构建更新请求
	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryThumbnail,
		Name:     "Thumbnail Configuration",
		Config: map[string]interface{}{
			"enabled":     settings.Enabled,
			"sizes":       settings.Sizes,
			"quality":     settings.Quality,
			"max_retries": settings.MaxRetries,
		},
		IsEnabled:   BoolPtr(settings.Enabled),
		IsDefault:   BoolPtr(true),
		Description: "Thumbnail generation settings",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	return err
}
