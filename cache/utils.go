package cache

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/anoixa/image-bed/database/models"
)

// addJitter 添加随机抖动（±10%），防止缓存雪崩
func addJitter(duration time.Duration) time.Duration {
	if duration <= 0 {
		return duration
	}
	jitter := time.Duration(rand.Int63n(int64(duration) / 10))
	return duration + jitter
}

const (
	// ImageCachePrefix 图片元数据缓存前缀
	ImageCachePrefix = "image_meta:"

	// UserCachePrefix 用户缓存前缀
	UserCachePrefix = "user:"

	// DeviceCachePrefix 设备缓存前缀
	DeviceCachePrefix = "device:"

	// StaticTokenCachePrefix static_token缓存前缀
	StaticTokenCachePrefix = "static_token:"

	// EmptyValueCachePrefix 空值缓存前缀
	EmptyValueCachePrefix = "empty:"

	// DefaultImageCacheExpiration 图片缓存过期时间
	DefaultImageCacheExpiration = 1 * time.Hour

	// DefaultUserCacheExpiration 用户缓存过期时间
	DefaultUserCacheExpiration = 30 * time.Minute

	// DefaultDeviceCacheExpiration 设备缓存过期时间
	DefaultDeviceCacheExpiration = 24 * time.Hour

	// DefaultStaticTokenCacheExpiration static_token缓存过期时间
	DefaultStaticTokenCacheExpiration = 1 * time.Hour

	// DefaultEmptyValueCacheExpiration 空值缓存过期时间
	DefaultEmptyValueCacheExpiration = 5 * time.Minute

	// AlbumCachePrefix 相册缓存前缀
	AlbumCachePrefix = "album:"

	// AlbumListCachePrefix 相册列表缓存前缀
	AlbumListCachePrefix = "album_list:"

	// AlbumListVersionPrefix 相册列表版本号缓存前缀
	AlbumListVersionPrefix = "album_list_version:"

	// DefaultAlbumCacheExpiration 相册缓存过期时间
	DefaultAlbumCacheExpiration = 30 * time.Minute

	// DefaultAlbumListCacheExpiration 相册列表缓存过期时间
	DefaultAlbumListCacheExpiration = 10 * time.Minute

	// DefaultAlbumListVersionExpiration 相册列表版本号过期时间
	DefaultAlbumListVersionExpiration = 30 * time.Minute

	// DefaultMaxCacheableImageSize 默认最大可缓存图片大小（10MB）
	DefaultMaxCacheableImageSize = 10 * 1024 * 1024
)

// HelperConfig 缓存辅助工具配置
type HelperConfig struct {
	ImageCacheTTL         time.Duration
	ImageDataCacheTTL     time.Duration
	MaxCacheableImageSize int64
}

// DefaultHelperConfig 返回默认配置
func DefaultHelperConfig() HelperConfig {
	return HelperConfig{
		ImageCacheTTL:         DefaultImageCacheExpiration,
		ImageDataCacheTTL:     1 * time.Hour,
		MaxCacheableImageSize: DefaultMaxCacheableImageSize,
	}
}

// Helper 缓存辅助工具结构
type Helper struct {
	provider Provider
	config   HelperConfig
}

// NewHelper 创建新的缓存辅助工具
func NewHelper(provider Provider, cfg ...HelperConfig) *Helper {
	c := DefaultHelperConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &Helper{
		provider: provider,
		config:   c,
	}
}

// CacheImage 缓存图片元数据
func (h *Helper) CacheImage(ctx context.Context, image *models.Image) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := ImageCachePrefix + image.Identifier
	return h.provider.Set(ctx, key, image, addJitter(h.config.ImageCacheTTL))
}

// GetCachedImage 获取缓存的图片元数据
func (h *Helper) GetCachedImage(ctx context.Context, identifier string, image *models.Image) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	key := ImageCachePrefix + identifier
	return h.provider.Get(ctx, key, image)
}

// CacheUser 缓存用户信息
func (h *Helper) CacheUser(ctx context.Context, user *models.User) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := UserCachePrefix + fmt.Sprintf("%d", user.ID)
	return h.provider.Set(ctx, key, user, addJitter(DefaultUserCacheExpiration))
}

// GetCachedUser 获取缓存的用户信息
func (h *Helper) GetCachedUser(ctx context.Context, userID uint, user *models.User) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)

	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.provider.Get(ctx, key, user)
}

// CacheDevice 缓存设备信息
func (h *Helper) CacheDevice(ctx context.Context, device *models.Device) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := DeviceCachePrefix + device.DeviceID
	return h.provider.Set(ctx, key, device, addJitter(DefaultDeviceCacheExpiration))
}

// GetCachedDevice 获取缓存的设备信息
func (h *Helper) GetCachedDevice(ctx context.Context, deviceID string, device *models.Device) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	key := DeviceCachePrefix + deviceID

	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.provider.Get(ctx, key, device)
}

// DeleteCachedImage 删除缓存的图片
func (h *Helper) DeleteCachedImage(ctx context.Context, identifier string) error {
	if h.provider == nil {
		return nil
	}

	key := ImageCachePrefix + identifier
	return h.provider.Delete(ctx, key)
}

// DeleteCachedUser 删除缓存的用户
func (h *Helper) DeleteCachedUser(ctx context.Context, userID uint) error {
	if h.provider == nil {
		return nil
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)
	return h.provider.Delete(ctx, key)
}

// DeleteCachedDevice 删除缓存的设备
func (h *Helper) DeleteCachedDevice(ctx context.Context, deviceID string) error {
	if h.provider == nil {
		return nil
	}

	key := DeviceCachePrefix + deviceID
	return h.provider.Delete(ctx, key)
}

// CacheStaticToken 缓存 static_token 和用户信息
func (h *Helper) CacheStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := StaticTokenCachePrefix + token
	return h.provider.Set(ctx, key, user, addJitter(DefaultStaticTokenCacheExpiration))
}

// GetCachedStaticToken 获取缓存的 static_token 用户信息
func (h *Helper) GetCachedStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	key := StaticTokenCachePrefix + token

	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.provider.Get(ctx, key, user)
}

// DeleteCachedStaticToken 删除缓存的 static_token
func (h *Helper) DeleteCachedStaticToken(ctx context.Context, token string) error {
	if h.provider == nil {
		return nil
	}

	key := StaticTokenCachePrefix + token
	return h.provider.Delete(ctx, key)
}

// CacheEmptyValue 缓存空值标记
func (h *Helper) CacheEmptyValue(ctx context.Context, key string) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.provider.Set(ctx, cacheKey, "EMPTY", addJitter(DefaultEmptyValueCacheExpiration))
}

// IsEmptyValue 检查是否为空值标记
func (h *Helper) IsEmptyValue(ctx context.Context, key string) (bool, error) {
	if h.provider == nil {
		return false, fmt.Errorf("cache provider not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	var value string
	err := h.provider.Get(ctx, cacheKey, &value)
	if err != nil {
		return false, err
	}

	return value == "EMPTY", nil
}

// DeleteEmptyValue 删除空值标记
func (h *Helper) DeleteEmptyValue(ctx context.Context, key string) error {
	if h.provider == nil {
		return nil
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.provider.Delete(ctx, cacheKey)
}

// CacheImageData 缓存图片数据（超过 MaxCacheableImageSize 的图片不会缓存）
func (h *Helper) CacheImageData(ctx context.Context, identifier string, imageData []byte) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	if int64(len(imageData)) > h.config.MaxCacheableImageSize {
		return nil
	}

	key := "image_data:" + identifier
	return h.provider.Set(ctx, key, imageData, addJitter(h.config.ImageDataCacheTTL))
}

// GetCachedImageData 获取缓存的图片数据
func (h *Helper) GetCachedImageData(ctx context.Context, identifier string) ([]byte, error) {
	if h.provider == nil {
		return nil, ErrCacheMiss
	}

	key := "image_data:" + identifier
	var imageData []byte
	err := h.provider.Get(ctx, key, &imageData)
	if err != nil {
		return nil, err
	}

	return imageData, nil
}

// DeleteCachedImageData 删除缓存的图片数据
func (h *Helper) DeleteCachedImageData(ctx context.Context, identifier string) error {
	if h.provider == nil {
		return nil
	}

	key := "image_data:" + identifier
	return h.provider.Delete(ctx, key)
}

// CacheAlbum 缓存相册信息
func (h *Helper) CacheAlbum(ctx context.Context, album *models.Album) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", album.ID)
	return h.provider.Set(ctx, key, album, addJitter(DefaultAlbumCacheExpiration))
}

// GetCachedAlbum 获取缓存的相册信息
func (h *Helper) GetCachedAlbum(ctx context.Context, albumID uint, album *models.Album) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", albumID)
	return h.provider.Get(ctx, key, album)
}

// DeleteCachedAlbum 删除缓存的相册
func (h *Helper) DeleteCachedAlbum(ctx context.Context, albumID uint) error {
	if h.provider == nil {
		return nil
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", albumID)
	return h.provider.Delete(ctx, key)
}

// getAlbumListVersion 获取用户相册列表的当前版本号
func (h *Helper) getAlbumListVersion(ctx context.Context, userID uint) int64 {
	if h.provider == nil {
		return 0
	}

	versionKey := fmt.Sprintf("%s%d", AlbumListVersionPrefix, userID)
	var version int64
	if err := h.provider.Get(ctx, versionKey, &version); err != nil {
		return 0
	}
	return version
}

// incrementAlbumListVersion 递增用户相册列表版本号（使旧缓存失效）
func (h *Helper) incrementAlbumListVersion(ctx context.Context, userID uint) error {
	if h.provider == nil {
		return nil
	}

	versionKey := fmt.Sprintf("%s%d", AlbumListVersionPrefix, userID)
	version := h.getAlbumListVersion(ctx, userID)
	version++
	return h.provider.Set(ctx, versionKey, version, addJitter(DefaultAlbumListVersionExpiration))
}

// CacheAlbumList 缓存用户相册列表（包含版本号）
func (h *Helper) CacheAlbumList(ctx context.Context, userID uint, page, limit int, data interface{}) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	version := h.getAlbumListVersion(ctx, userID)
	key := fmt.Sprintf("%suser:%d:v%d:page:%d:limit:%d", AlbumListCachePrefix, userID, version, page, limit)
	return h.provider.Set(ctx, key, data, addJitter(DefaultAlbumListCacheExpiration))
}

// GetCachedAlbumList 获取缓存的用户相册列表（检查版本号）
func (h *Helper) GetCachedAlbumList(ctx context.Context, userID uint, page, limit int, dest interface{}) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	version := h.getAlbumListVersion(ctx, userID)
	key := fmt.Sprintf("%suser:%d:v%d:page:%d:limit:%d", AlbumListCachePrefix, userID, version, page, limit)
	return h.provider.Get(ctx, key, dest)
}

// DeleteCachedAlbumList 删除用户的所有相册列表缓存（通过递增版本号）
func (h *Helper) DeleteCachedAlbumList(ctx context.Context, userID uint) error {
	if h.provider == nil {
		return nil
	}

	// 使用版本号机制使所有旧缓存失效
	// 无需遍历删除，旧版本号的缓存会自动过期
	return h.incrementAlbumListVersion(ctx, userID)
}
