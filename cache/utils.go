package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
)

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
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := ImageCachePrefix + image.Identifier
	cfg := config.Get()
	ttl := DefaultImageCacheExpiration
	if cfg != nil && cfg.Server.CacheConfig.ImageCacheTTL > 0 {
		ttl = time.Duration(cfg.Server.CacheConfig.ImageCacheTTL) * time.Second
	}
	return h.factory.Set(ctx, key, image, ttl)
}

// GetCachedImage 获取缓存的图片元数据
func (h *Helper) GetCachedImage(ctx context.Context, identifier string, image *models.Image) error {
	if h.factory == nil || h.factory.provider == nil {
		return ErrCacheMiss
	}

	key := ImageCachePrefix + identifier
	return h.factory.Get(ctx, key, image)
}

// CacheUser 缓存用户信息
func (h *Helper) CacheUser(ctx context.Context, user *models.User) error {
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := UserCachePrefix + fmt.Sprintf("%d", user.ID)
	return h.factory.Set(ctx, key, user, DefaultUserCacheExpiration)
}

// GetCachedUser 获取缓存的用户信息
func (h *Helper) GetCachedUser(ctx context.Context, userID uint, user *models.User) error {
	if h.factory == nil || h.factory.provider == nil {
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
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := DeviceCachePrefix + device.DeviceID
	return h.factory.Set(ctx, key, device, DefaultDeviceCacheExpiration)
}

// GetCachedDevice 获取缓存的设备信息
func (h *Helper) GetCachedDevice(ctx context.Context, deviceID string, device *models.Device) error {
	if h.factory == nil || h.factory.provider == nil {
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
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	key := ImageCachePrefix + identifier
	return h.factory.Delete(ctx, key)
}

// DeleteCachedUser 删除缓存的用户
func (h *Helper) DeleteCachedUser(ctx context.Context, userID uint) error {
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)
	return h.factory.Delete(ctx, key)
}

// DeleteCachedDevice 删除缓存的设备
func (h *Helper) DeleteCachedDevice(ctx context.Context, deviceID string) error {
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	key := DeviceCachePrefix + deviceID
	return h.factory.Delete(ctx, key)
}

// CacheStaticToken 缓存 static_token 和用户信息
func (h *Helper) CacheStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := StaticTokenCachePrefix + token
	return h.factory.Set(ctx, key, user, DefaultStaticTokenCacheExpiration)
}

// GetCachedStaticToken 获取缓存的 static_token 用户信息
func (h *Helper) GetCachedStaticToken(ctx context.Context, token string, user *models.User) error {
	if h.factory == nil || h.factory.provider == nil {
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
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	key := StaticTokenCachePrefix + token
	return h.factory.Delete(ctx, key)
}

// CacheEmptyValue 缓存空值标记
func (h *Helper) CacheEmptyValue(ctx context.Context, key string) error {
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.factory.Set(ctx, cacheKey, "EMPTY", DefaultEmptyValueCacheExpiration)
}

// IsEmptyValue 检查是否为空值标记
func (h *Helper) IsEmptyValue(ctx context.Context, key string) (bool, error) {
	if h.factory == nil || h.factory.provider == nil {
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
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	cacheKey := EmptyValueCachePrefix + key
	return h.factory.Delete(ctx, cacheKey)
}

// CacheImageData 缓存图片数据
func (h *Helper) CacheImageData(ctx context.Context, identifier string, imageData []byte) error {
	if h.factory == nil || h.factory.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	key := "image_data:" + identifier
	cfg := config.Get()
	expiration := 1 * time.Hour
	if cfg != nil && cfg.Server.CacheConfig.ImageDataCacheTTL > 0 {
		expiration = time.Duration(cfg.Server.CacheConfig.ImageDataCacheTTL) * time.Second
	}
	return h.factory.Set(ctx, key, imageData, expiration)
}

// GetCachedImageData 获取缓存的图片数据
func (h *Helper) GetCachedImageData(ctx context.Context, identifier string) ([]byte, error) {
	if h.factory == nil || h.factory.provider == nil {
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
	if h.factory == nil || h.factory.provider == nil {
		return nil
	}

	key := "image_data:" + identifier
	return h.factory.Delete(ctx, key)
}
