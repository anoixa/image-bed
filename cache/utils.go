package cache

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/anoixa/image-bed/config"
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
	// ImageCachePrefix 图片缓存前缀
	ImageCachePrefix = "image:"

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
)

// Helper 缓存辅助工具结构
type Helper struct {
	factory *Factory
}

// NewHelper 创建新的缓存辅助工具
func NewHelper(factory *Factory) *Helper {
	return &Helper{factory: factory}
}

// CacheImage 缓存图片元数据
func (h *Helper) CacheImage(ctx context.Context, image *models.Image) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := ImageCachePrefix + image.Identifier
	cfg := config.Get()
	ttl := DefaultImageCacheExpiration
	if cfg != nil && cfg.CacheImageCacheTTL > 0 {
		ttl = time.Duration(cfg.CacheImageCacheTTL) * time.Second
	}
	return h.factory.Set(ctx, key, image, addJitter(ttl))
}

// GetCachedImage 获取缓存的图片元数据
func (h *Helper) GetCachedImage(ctx context.Context, identifier string, image *models.Image) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	key := ImageCachePrefix + identifier
	return h.factory.Get(ctx, key, image)
}

// CacheUser 缓存用户信息
func (h *Helper) CacheUser(ctx context.Context, user *models.User) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := UserCachePrefix + fmt.Sprintf("%d", user.ID)
	return h.factory.Set(ctx, key, user, addJitter(DefaultUserCacheExpiration))
}

// GetCachedUser 获取缓存的用户信息
func (h *Helper) GetCachedUser(ctx context.Context, userID uint, user *models.User) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)

	// 检查是否为空值
	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.factory.Get(ctx, key, user)
}

// CacheDevice 缓存设备信息
func (h *Helper) CacheDevice(ctx context.Context, device *models.Device) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := DeviceCachePrefix + device.DeviceID
	return h.factory.Set(ctx, key, device, addJitter(DefaultDeviceCacheExpiration))
}

// GetCachedDevice 获取缓存的设备信息
func (h *Helper) GetCachedDevice(ctx context.Context, deviceID string, device *models.Device) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	key := DeviceCachePrefix + deviceID

	// 检查是否为空值
	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.factory.Get(ctx, key, device)
}

// DeleteCachedImage 删除缓存的图片
func (h *Helper) DeleteCachedImage(ctx context.Context, identifier string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := ImageCachePrefix + identifier
	return h.factory.Delete(ctx, key)
}

// DeleteCachedUser 删除缓存的用户
func (h *Helper) DeleteCachedUser(ctx context.Context, userID uint) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)
	return h.factory.Delete(ctx, key)
}

// DeleteCachedDevice 删除缓存的设备
func (h *Helper) DeleteCachedDevice(ctx context.Context, deviceID string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := DeviceCachePrefix + deviceID
	return h.factory.Delete(ctx, key)
}

// CacheStaticToken 缓存 static_token 和用户信息
func (h *Helper) CacheStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := StaticTokenCachePrefix + token
	return h.factory.Set(ctx, key, user, addJitter(DefaultStaticTokenCacheExpiration))
}

// GetCachedStaticToken 获取缓存的 static_token 用户信息
func (h *Helper) GetCachedStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	key := StaticTokenCachePrefix + token

	// 检查是否为空值
	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.factory.Get(ctx, key, user)
}

// DeleteCachedStaticToken 删除缓存的 static_token
func (h *Helper) DeleteCachedStaticToken(ctx context.Context, token string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := StaticTokenCachePrefix + token
	return h.factory.Delete(ctx, key)
}

// CacheEmptyValue 缓存空值标记
func (h *Helper) CacheEmptyValue(ctx context.Context, key string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.factory.Set(ctx, cacheKey, "EMPTY", addJitter(DefaultEmptyValueCacheExpiration))
}

// IsEmptyValue 检查是否为空值标记
func (h *Helper) IsEmptyValue(ctx context.Context, key string) (bool, error) {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return false, fmt.Errorf("cache provider not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	var value string
	err := h.factory.Get(ctx, cacheKey, &value)
	if err != nil {
		return false, err
	}

	return value == "EMPTY", nil
}

// DeleteEmptyValue 删除空值标记
func (h *Helper) DeleteEmptyValue(ctx context.Context, key string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.factory.Delete(ctx, cacheKey)
}

// CacheImageData 缓存图片数据
func (h *Helper) CacheImageData(ctx context.Context, identifier string, imageData []byte) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := "image_data:" + identifier
	cfg := config.Get()
	expiration := 1 * time.Hour
	if cfg != nil && cfg.CacheImageDataCacheTTL > 0 {
		expiration = time.Duration(cfg.CacheImageDataCacheTTL) * time.Second
	}
	return h.factory.Set(ctx, key, imageData, addJitter(expiration))
}

// GetCachedImageData 获取缓存的图片数据
func (h *Helper) GetCachedImageData(ctx context.Context, identifier string) ([]byte, error) {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil, ErrCacheMiss
	}

	key := "image_data:" + identifier
	var imageData []byte
	err := h.factory.Get(ctx, key, &imageData)
	if err != nil {
		return nil, err
	}

	return imageData, nil
}

// DeleteCachedImageData 删除缓存的图片数据
func (h *Helper) DeleteCachedImageData(ctx context.Context, identifier string) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := "image_data:" + identifier
	return h.factory.Delete(ctx, key)
}

// CacheAlbum 缓存相册信息
func (h *Helper) CacheAlbum(ctx context.Context, album *models.Album) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", album.ID)
	return h.factory.Set(ctx, key, album, addJitter(DefaultAlbumCacheExpiration))
}

// GetCachedAlbum 获取缓存的相册信息
func (h *Helper) GetCachedAlbum(ctx context.Context, albumID uint, album *models.Album) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", albumID)
	return h.factory.Get(ctx, key, album)
}

// DeleteCachedAlbum 删除缓存的相册
func (h *Helper) DeleteCachedAlbum(ctx context.Context, albumID uint) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	key := AlbumCachePrefix + fmt.Sprintf("%d", albumID)
	return h.factory.Delete(ctx, key)
}

// getAlbumListVersion 获取用户相册列表的当前版本号
func (h *Helper) getAlbumListVersion(ctx context.Context, userID uint) int64 {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return 0
	}

	versionKey := fmt.Sprintf("%s%d", AlbumListVersionPrefix, userID)
	var version int64
	if err := h.factory.Get(ctx, versionKey, &version); err != nil {
		return 0
	}
	return version
}

// incrementAlbumListVersion 递增用户相册列表版本号（使旧缓存失效）
func (h *Helper) incrementAlbumListVersion(ctx context.Context, userID uint) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	versionKey := fmt.Sprintf("%s%d", AlbumListVersionPrefix, userID)
	version := h.getAlbumListVersion(ctx, userID)
	version++
	return h.factory.Set(ctx, versionKey, version, addJitter(DefaultAlbumListVersionExpiration))
}

// CacheAlbumList 缓存用户相册列表（包含版本号）
func (h *Helper) CacheAlbumList(ctx context.Context, userID uint, page, limit int, data interface{}) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	version := h.getAlbumListVersion(ctx, userID)
	key := fmt.Sprintf("%suser:%d:v%d:page:%d:limit:%d", AlbumListCachePrefix, userID, version, page, limit)
	return h.factory.Set(ctx, key, data, addJitter(DefaultAlbumListCacheExpiration))
}

// GetCachedAlbumList 获取缓存的用户相册列表（检查版本号）
func (h *Helper) GetCachedAlbumList(ctx context.Context, userID uint, page, limit int, dest interface{}) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return ErrCacheMiss
	}

	version := h.getAlbumListVersion(ctx, userID)
	key := fmt.Sprintf("%suser:%d:v%d:page:%d:limit:%d", AlbumListCachePrefix, userID, version, page, limit)
	return h.factory.Get(ctx, key, dest)
}

// DeleteCachedAlbumList 删除用户的所有相册列表缓存（通过递增版本号）
func (h *Helper) DeleteCachedAlbumList(ctx context.Context, userID uint) error {
	if h.factory == nil || h.factory.GetProvider() == nil {
		return nil
	}

	// 使用版本号机制使所有旧缓存失效
	// 无需遍历删除，旧版本号的缓存会自动过期
	return h.incrementAlbumListVersion(ctx, userID)
}
