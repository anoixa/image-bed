package cache

import (
	"fmt"
	"time"

	"github.com/anoixa/image-bed/cache/types"
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

// CacheImage 缓存图片元数据
func CacheImage(image *models.Image) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := ImageCachePrefix + image.Identifier
	return GlobalManager.Set(key, image, DefaultImageCacheExpiration)
}

// GetCachedImage 获取缓存的图片元数据
func GetCachedImage(identifier string, image *models.Image) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := ImageCachePrefix + identifier
	return GlobalManager.Get(key, image)
}

// CacheUser 缓存用户信息
func CacheUser(user *models.User) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := UserCachePrefix + fmt.Sprintf("%d", user.ID)
	return GlobalManager.Set(key, user, DefaultUserCacheExpiration)
}

// GetCachedUser 获取缓存的用户信息
func GetCachedUser(userID uint, user *models.User) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)

	// 检查是否为空值
	if isEmpty, err := IsEmptyValue(key); err == nil && isEmpty {
		return types.ErrCacheMiss
	}

	return GlobalManager.Get(key, user)
}

// CacheDevice 缓存设备信息
func CacheDevice(device *models.Device) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := DeviceCachePrefix + device.DeviceID
	return GlobalManager.Set(key, device, DefaultDeviceCacheExpiration)
}

// GetCachedDevice 获取缓存的设备信息
func GetCachedDevice(deviceID string, device *models.Device) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := DeviceCachePrefix + deviceID

	// 检查是否为空值
	if isEmpty, err := IsEmptyValue(key); err == nil && isEmpty {
		return types.ErrCacheMiss
	}

	return GlobalManager.Get(key, device)
}

// DeleteCachedImage 删除缓存的图片
func DeleteCachedImage(identifier string) error {
	if GlobalManager == nil {
		return nil // 如果缓存未初始化，直接返回
	}

	key := ImageCachePrefix + identifier
	return GlobalManager.Delete(key)
}

// DeleteCachedUser 删除缓存的用户
func DeleteCachedUser(userID uint) error {
	if GlobalManager == nil {
		return nil // 如果缓存未初始化，直接返回
	}

	key := UserCachePrefix + fmt.Sprintf("%d", userID)
	return GlobalManager.Delete(key)
}

// DeleteCachedDevice 删除缓存的设备
func DeleteCachedDevice(deviceID string) error {
	if GlobalManager == nil {
		return nil // 如果缓存未初始化，直接返回
	}

	key := DeviceCachePrefix + deviceID
	return GlobalManager.Delete(key)
}

// CacheStaticToken 缓存static_token和用户信息
func CacheStaticToken(token string, user *models.User) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := StaticTokenCachePrefix + token
	return GlobalManager.Set(key, user, DefaultStaticTokenCacheExpiration)
}

// GetCachedStaticToken 获取缓存的static_token用户信息
func GetCachedStaticToken(token string, user *models.User) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	key := StaticTokenCachePrefix + token

	// 检查是否为空值
	if isEmpty, err := IsEmptyValue(key); err == nil && isEmpty {
		return types.ErrCacheMiss
	}

	return GlobalManager.Get(key, user)
}

// DeleteCachedStaticToken 删除缓存的static_token
func DeleteCachedStaticToken(token string) error {
	if GlobalManager == nil {
		return nil // 如果缓存未初始化，直接返回
	}

	key := StaticTokenCachePrefix + token
	return GlobalManager.Delete(key)
}

// CacheEmptyValue 缓存空值标记
func CacheEmptyValue(key string) error {
	if GlobalManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	return GlobalManager.Set(cacheKey, "EMPTY", DefaultEmptyValueCacheExpiration)
}

// IsEmptyValue 检查是否为空值标记
func IsEmptyValue(key string) (bool, error) {
	if GlobalManager == nil {
		return false, fmt.Errorf("cache manager not initialized")
	}

	cacheKey := EmptyValueCachePrefix + key
	var value string
	err := GlobalManager.Get(cacheKey, &value)
	if err != nil {
		return false, err
	}

	return value == "EMPTY", nil
}

// DeleteEmptyValue 删除空值标记
func DeleteEmptyValue(key string) error {
	if GlobalManager == nil {
		return nil
	}

	cacheKey := EmptyValueCachePrefix + key
	return GlobalManager.Delete(cacheKey)
}

// IsCacheMiss 判断是否为缓存未命中错误
func IsCacheMiss(err error) bool {
	return types.IsCacheMiss(err)
}
