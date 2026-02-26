package config

import (
	"context"
	"time"

	"github.com/anoixa/image-bed/database/models"
)

// RandomSourceAlbumConfigKey 随机图源相册配置键
const RandomSourceAlbumConfigKey = "random_source_album"

// RandomAlbumConfig 随机图源相册配置
type RandomAlbumConfig struct {
	AlbumID uint `json:"album_id"`
}

// GetRandomSourceAlbum 获取随机图源相册ID
func (m *Manager) GetRandomSourceAlbum() uint {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	config, err := m.repo.GetByKey(ctx, RandomSourceAlbumConfigKey)
	if err != nil || config == nil {
		return 0
	}

	// 解密配置
	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return 0
	}

	// 解析相册ID
	if albumID, ok := configMap["album_id"]; ok {
		switch v := albumID.(type) {
		case float64:
			return uint(v)
		case int:
			return uint(v)
		case uint:
			return v
		}
	}

	return 0
}

// SetRandomSourceAlbum 设置随机图源相册ID
func (m *Manager) SetRandomSourceAlbum(albumID uint) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := map[string]interface{}{
		"album_id": albumID,
	}

	// 检查是否已存在
	existing, err := m.repo.GetByKey(ctx, RandomSourceAlbumConfigKey)
	if err != nil || existing == nil {
		// 创建新配置
		return m.createRandomAlbumConfig(ctx, config)
	}

	// 更新现有配置
	return m.updateRandomAlbumConfig(ctx, existing.ID, config)
}

// createRandomAlbumConfig 创建随机图源相册配置
func (m *Manager) createRandomAlbumConfig(ctx context.Context, config map[string]interface{}) error {
	req := &models.SystemConfigStoreRequest{
		Category:    "random",
		Name:        "source_album",
		Config:      config,
		IsEnabled:   boolPtr(true),
		Description: "随机图片API源相册配置",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	return err
}

// updateRandomAlbumConfig 更新随机图源相册配置
func (m *Manager) updateRandomAlbumConfig(ctx context.Context, id uint, config map[string]interface{}) error {
	req := &models.SystemConfigStoreRequest{
		Category:    "random",
		Name:        "source_album",
		Config:      config,
		IsEnabled:   boolPtr(true),
		Description: "随机图片API源相册配置",
	}

	_, err := m.UpdateConfig(ctx, id, req)
	return err
}

func boolPtr(b bool) *bool {
	return &b
}
