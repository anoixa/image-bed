package config

import (
	"context"
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// RandomSourceAlbumConfigKey 随机图源相册配置键
const RandomSourceAlbumConfigKey = "system:random_source_album"

const (
	randomSourceAlbumConfigName      = "random_source_album"
	legacyRandomSourceAlbumConfigKey = "system:" + RandomSourceAlbumConfigKey
)

// RandomAlbumConfig 随机图源相册配置
type RandomAlbumConfig struct {
	AlbumID          uint `json:"album_id"`
	IncludeAllPublic bool `json:"include_all_public"`
	Enabled          bool `json:"enabled"`
}

// GetRandomSourceAlbum 获取随机图源相册ID
func (m *Manager) GetRandomSourceAlbum() uint {
	config := m.getRandomAlbumConfig()
	if config == nil {
		return 0
	}
	return config.AlbumID
}

// GetRandomIncludeAllPublic 获取是否包含所有公开图片的配置
func (m *Manager) GetRandomIncludeAllPublic() bool {
	config := m.getRandomAlbumConfig()
	if config == nil {
		return false
	}
	return config.IncludeAllPublic
}

// GetRandomAPIEnabled 获取随机图片 API 是否启用。未配置时默认启用，兼容历史部署。
func (m *Manager) GetRandomAPIEnabled() bool {
	config := m.getRandomAlbumConfig()
	if config == nil {
		return true
	}
	return config.Enabled
}

// getRandomAlbumConfig 获取随机图源相册完整配置
func (m *Manager) getRandomAlbumConfig() *RandomAlbumConfig {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	config, err := m.getRandomAlbumSystemConfig(ctx)
	if err != nil || config == nil {
		return nil
	}

	// 解密配置
	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil
	}

	result := &RandomAlbumConfig{Enabled: true}

	// 解析相册ID
	if albumID, ok := configMap["album_id"]; ok {
		switch v := albumID.(type) {
		case float64:
			result.AlbumID = uint(v)
		case int:
			result.AlbumID = uint(v)
		case uint:
			result.AlbumID = v
		}
	}

	// 解析是否包含所有公开图片
	if includeAllPublic, ok := configMap["include_all_public"]; ok {
		switch v := includeAllPublic.(type) {
		case bool:
			result.IncludeAllPublic = v
		}
	}

	if enabled, ok := configMap["enabled"]; ok {
		switch v := enabled.(type) {
		case bool:
			result.Enabled = v
		}
	}

	return result
}

// SetRandomSourceAlbum 设置随机图源相册配置
func (m *Manager) SetRandomSourceAlbum(albumID uint, includeAllPublic bool, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := map[string]any{
		"album_id":           albumID,
		"include_all_public": includeAllPublic,
		"enabled":            enabled,
	}

	existing, err := m.getRandomAlbumSystemConfig(ctx)
	if err != nil || existing == nil {
		// 创建新配置
		return m.createRandomAlbumConfig(ctx, config)
	}

	// 更新现有配置
	return m.updateRandomAlbumConfig(ctx, existing.ID, config)
}

// createRandomAlbumConfig 创建随机图源相册配置
func (m *Manager) createRandomAlbumConfig(ctx context.Context, config map[string]any) error {
	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategorySystem,
		Name:        randomSourceAlbumConfigName,
		Config:      config,
		IsEnabled:   boolPtr(true),
		Description: "随机图片API源相册配置",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	return err
}

// updateRandomAlbumConfig 更新随机图源相册配置
func (m *Manager) updateRandomAlbumConfig(ctx context.Context, id uint, config map[string]any) error {
	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategorySystem,
		Name:        randomSourceAlbumConfigName,
		Config:      config,
		IsEnabled:   boolPtr(true),
		Description: "随机图片API源相册配置",
	}

	_, err := m.UpdateConfig(ctx, id, req)
	return err
}

func (m *Manager) getRandomAlbumSystemConfig(ctx context.Context) (*models.SystemConfig, error) {
	config, err := m.repo.GetByKey(ctx, RandomSourceAlbumConfigKey)
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	config, err = m.repo.GetByKey(ctx, legacyRandomSourceAlbumConfigKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	config.Key = RandomSourceAlbumConfigKey
	config.Name = randomSourceAlbumConfigName
	if err := m.repo.Update(ctx, config); err != nil {
		configManagerLog.Warnf("Failed to migrate random source album config key: %v", err)
	}
	return config, nil
}

func boolPtr(b bool) *bool {
	return &b
}
