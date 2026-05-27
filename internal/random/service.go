package random

import (
	"sync"
	"time"

	configSvc "github.com/anoixa/image-bed/config/db"
)

// Service 随机图片服务
type Service struct {
	configManager *configSvc.Manager
	mu            sync.RWMutex
	cache         *albumCache
}

type SourceConfig struct {
	AlbumID          uint
	IncludeAllPublic bool
	Enabled          bool
}

// albumCache 缓存结构
type albumCache struct {
	albumID          uint
	includeAllPublic bool
	enabled          bool
	expiresAt        time.Time
}

const cacheTTL = 5 * time.Minute

// NewService 创建随机图片服务
func NewService(configManager *configSvc.Manager) *Service {
	service := &Service{
		configManager: configManager,
	}

	service.warmCache()
	return service
}

// GetSourceAlbum 获取随机图源相册ID和是否包含所有公开图片的配置
func (s *Service) GetSourceAlbum() (uint, bool) {
	config := s.GetSourceConfig()
	return config.AlbumID, config.IncludeAllPublic
}

// GetSourceConfig 获取随机图片 API 配置
func (s *Service) GetSourceConfig() SourceConfig {
	s.mu.RLock()
	if s.cache != nil && time.Now().Before(s.cache.expiresAt) {
		config := SourceConfig{
			AlbumID:          s.cache.albumID,
			IncludeAllPublic: s.cache.includeAllPublic,
			Enabled:          s.cache.enabled,
		}
		s.mu.RUnlock()
		return config
	}
	s.mu.RUnlock()

	// 从数据库加载
	config := SourceConfig{Enabled: true}
	if s.configManager != nil {
		config.AlbumID = s.configManager.GetRandomSourceAlbum()
		config.IncludeAllPublic = s.configManager.GetRandomIncludeAllPublic()
		config.Enabled = s.configManager.GetRandomAPIEnabled()
	}
	config.AlbumID, config.IncludeAllPublic = normalizeSourceAlbum(config.AlbumID, config.IncludeAllPublic)

	s.mu.Lock()
	s.cache = &albumCache{
		albumID:          config.AlbumID,
		includeAllPublic: config.IncludeAllPublic,
		enabled:          config.Enabled,
		expiresAt:        time.Now().Add(cacheTTL),
	}
	s.mu.Unlock()

	return config
}

// SetSourceAlbum 设置随机图源相册ID和是否包含所有公开图片的配置（数据库+缓存）
func (s *Service) SetSourceAlbum(albumID uint, includeAllPublic bool) error {
	return s.SetSourceConfig(SourceConfig{
		AlbumID:          albumID,
		IncludeAllPublic: includeAllPublic,
		Enabled:          true,
	})
}

// SetSourceConfig 设置随机图片 API 配置（数据库+缓存）
func (s *Service) SetSourceConfig(config SourceConfig) error {
	albumID := config.AlbumID
	includeAllPublic := config.IncludeAllPublic
	albumID, includeAllPublic = normalizeSourceAlbum(albumID, includeAllPublic)

	// 保存到数据库
	if s.configManager != nil {
		if err := s.configManager.SetRandomSourceAlbum(albumID, includeAllPublic, config.Enabled); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.cache = &albumCache{
		albumID:          albumID,
		includeAllPublic: includeAllPublic,
		enabled:          config.Enabled,
		expiresAt:        time.Now().Add(cacheTTL),
	}
	s.mu.Unlock()

	return nil
}

// warmCache 预热缓存
func (s *Service) warmCache() {
	_, _ = s.GetSourceAlbum()
}

func normalizeSourceAlbum(albumID uint, includeAllPublic bool) (uint, bool) {
	if albumID > 0 {
		return albumID, false
	}
	return albumID, includeAllPublic
}
