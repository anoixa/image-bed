package random

import (
	"time"

	configSvc "github.com/anoixa/image-bed/config/db"
)

// Service 随机图片服务
type Service struct {
	configManager *configSvc.Manager
	cache         *albumCache
}

// albumCache 缓存结构
type albumCache struct {
	albumID   uint
	expiresAt time.Time
}

const cacheTTL = 5 * time.Minute

// NewService 创建随机图片服务
func NewService(configManager *configSvc.Manager) *Service {
	service := &Service{
		configManager: configManager,
	}
	// 启动时预热缓存
	service.warmCache()
	return service
}

// GetSourceAlbum 获取随机图源相册ID（缓存+数据库）
func (s *Service) GetSourceAlbum() uint {
	// 检查缓存
	if s.cache != nil && time.Now().Before(s.cache.expiresAt) {
		return s.cache.albumID
	}

	// 从数据库加载
	var albumID uint
	if s.configManager != nil {
		albumID = s.configManager.GetRandomSourceAlbum()
	}

	// 更新缓存
	s.cache = &albumCache{
		albumID:   albumID,
		expiresAt: time.Now().Add(cacheTTL),
	}

	return albumID
}

// SetSourceAlbum 设置随机图源相册ID（数据库+缓存）
func (s *Service) SetSourceAlbum(albumID uint) error {
	// 保存到数据库
	if s.configManager != nil {
		if err := s.configManager.SetRandomSourceAlbum(albumID); err != nil {
			return err
		}
	}

	// 更新缓存
	s.cache = &albumCache{
		albumID:   albumID,
		expiresAt: time.Now().Add(cacheTTL),
	}

	return nil
}

// warmCache 预热缓存
func (s *Service) warmCache() {
	_ = s.GetSourceAlbum()
}
