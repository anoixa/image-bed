package cache

import (
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
)

const (
	// ImageCachePrefix 图片缓存前缀
	ImageCachePrefix = "image:"

	// UserCachePrefix 用户缓存前缀
	UserCachePrefix = "user:"

	// DeviceCachePrefix 设备缓存前缀
	DeviceCachePrefix = "device:"

	// DefaultImageCacheExpiration 图片缓存过期时间
	DefaultImageCacheExpiration = 1 * time.Hour

	// DefaultUserCacheExpiration 用户缓存过期时间
	DefaultUserCacheExpiration = 30 * time.Minute

	// DefaultDeviceCacheExpiration 设备缓存过期时间
	DefaultDeviceCacheExpiration = 24 * time.Hour
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
