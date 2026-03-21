package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"github.com/mitchellh/mapstructure"
	"gorm.io/gorm"
)

// UserSettings 用户设置配置
type UserSettings struct {
	// WebP 转换设置
	WebPEnabled bool `json:"webp_enabled" mapstructure:"webp_enabled"` // 是否启用WebP转换

	// 上传设置
	DefaultAlbumID         uint `json:"default_album_id" mapstructure:"default_album_id"`                   // 默认上传相册ID，0表示不指定
	ConcurrentUploadLimit  int  `json:"concurrent_upload_limit" mapstructure:"concurrent_upload_limit"`     // 并发上传限制
	MaxFileSizeMB          int  `json:"max_file_size_mb" mapstructure:"max_file_size_mb"`                   // 最大文件大小(MB)
	ImageQuality           int  `json:"image_quality" mapstructure:"image_quality"`                         // 图片保存质量(1-100)

	// API 设置
	APIKeyEnabled bool `json:"api_key_enabled" mapstructure:"api_key_enabled"` // 是否启用API Key

	// 界面设置
	DefaultVisibility string `json:"default_visibility" mapstructure:"default_visibility"` // 默认可见性: public/private
}

// DefaultUserSettings 默认用户设置
func DefaultUserSettings() *UserSettings {
	return &UserSettings{
		WebPEnabled:           true,
		DefaultAlbumID:        0,
		ConcurrentUploadLimit: 3,
		MaxFileSizeMB:         50,
		ImageQuality:          85,
		APIKeyEnabled:         true,
		DefaultVisibility:     "public",
	}
}

// Validate 验证配置有效性
func (s *UserSettings) Validate() error {
	if s.ConcurrentUploadLimit < 1 || s.ConcurrentUploadLimit > 10 {
		return fmt.Errorf("concurrent upload limit must be between 1 and 10")
	}
	if s.MaxFileSizeMB < 1 || s.MaxFileSizeMB > 500 {
		return fmt.Errorf("max file size must be between 1 and 500 MB")
	}
	if s.ImageQuality < 1 || s.ImageQuality > 100 {
		return fmt.Errorf("image quality must be between 1 and 100")
	}
	if s.DefaultVisibility != "public" && s.DefaultVisibility != "private" {
		return fmt.Errorf("default visibility must be 'public' or 'private'")
	}
	return nil
}

const userSettingsKey = "system:user_settings"

// GetUserSettings 获取用户设置
func (m *Manager) GetUserSettings(ctx context.Context) (*UserSettings, error) {
	// 先从缓存获取
	if cached := m.cache.GetUserSettings(); cached != nil {
		return cached, nil
	}

	config, err := m.repo.GetByKey(ctx, userSettingsKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := m.ensureDefaultUserSettings(ctx); err != nil {
				return nil, fmt.Errorf("failed to create default user settings: %w", err)
			}
			config, err = m.repo.GetByKey(ctx, userSettingsKey)
			if err != nil {
				return nil, fmt.Errorf("failed to get user settings after creation: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get user settings: %w", err)
		}
	}

	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt user settings: %w", err)
	}

	settings := &UserSettings{}
	if err := mapstructure.Decode(configMap, settings); err != nil {
		return nil, fmt.Errorf("failed to decode user settings: %w", err)
	}

	m.cache.SetUserSettings(settings)
	return settings, nil
}

// SaveUserSettings 保存用户设置
func (m *Manager) SaveUserSettings(ctx context.Context, settings *UserSettings, userID uint) error {
	if err := settings.Validate(); err != nil {
		return err
	}

	config, err := m.repo.GetByKey(ctx, userSettingsKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return m.ensureDefaultUserSettings(ctx)
		}
		return err
	}

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategorySystem,
		Name:     "User Settings",
		Config: map[string]any{
			"webp_enabled":            settings.WebPEnabled,
			"default_album_id":        settings.DefaultAlbumID,
			"concurrent_upload_limit": settings.ConcurrentUploadLimit,
			"max_file_size_mb":        settings.MaxFileSizeMB,
			"image_quality":           settings.ImageQuality,
			"api_key_enabled":         settings.APIKeyEnabled,
			"default_visibility":      settings.DefaultVisibility,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "User application settings",
	}

	_, err = m.UpdateConfig(ctx, config.ID, req)
	if err == nil {
		m.cache.SetUserSettings(settings)
	}
	return err
}

// ensureDefaultUserSettings 确保默认用户设置存在
func (m *Manager) ensureDefaultUserSettings(ctx context.Context) error {
	defaultSettings := DefaultUserSettings()

	req := &models.SystemConfigStoreRequest{
		Category: models.ConfigCategorySystem,
		Name:     "User Settings",
		Config: map[string]any{
			"webp_enabled":            defaultSettings.WebPEnabled,
			"default_album_id":        defaultSettings.DefaultAlbumID,
			"concurrent_upload_limit": defaultSettings.ConcurrentUploadLimit,
			"max_file_size_mb":        defaultSettings.MaxFileSizeMB,
			"image_quality":           defaultSettings.ImageQuality,
			"api_key_enabled":         defaultSettings.APIKeyEnabled,
			"default_visibility":      defaultSettings.DefaultVisibility,
		},
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "User application settings",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to create default user settings: %w", err)
	}

	return nil
}
