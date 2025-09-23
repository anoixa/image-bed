package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/cache/types"
	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var deviceGroup singleflight.Group

// CreateLoginDevice Create device record
func CreateLoginDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken)) // 哈希原始 token
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := dbcore.Transaction(func(tx *gorm.DB) error {
		device := &models.Device{
			UserID:       userID,
			RefreshToken: hashedToken,
			Expiry:       refreshTokenExpiry,
			DeviceID:     deviceID,
		}

		if err := tx.Create(device).Error; err != nil {
			return fmt.Errorf("failed to create device record in transaction: %w", err)
		}

		// 缓存设备信息
		if cacheErr := cache.CacheDevice(device); cacheErr != nil {
			// 记录错误但不中断操作
			fmt.Printf("Failed to cache device: %v\n", cacheErr)
		}

		return nil
	})
	return err
}

// GetDeviceByRefreshTokenAndDeviceID Get device by refresh token and device id
func GetDeviceByRefreshTokenAndDeviceID(refreshToken string, deviceID string) (*models.Device, error) {
	// 首先尝试从缓存中获取设备信息
	var device models.Device
	err := cache.GetCachedDevice(deviceID, &device)
	if err == nil {
		// 缓存命中
		hasher := sha256.New()
		hasher.Write([]byte(refreshToken))
		hashedToken := hex.EncodeToString(hasher.Sum(nil))

		if device.RefreshToken == hashedToken {
			return &device, nil
		}
	} else if !types.IsCacheMiss(err) {
		fmt.Printf("Cache error: %v\n", err)
	}

	// 使用singleflight防止缓存击穿
	val, err, _ := deviceGroup.Do(fmt.Sprintf("device_%s", deviceID), func() (interface{}, error) {
		db := dbcore.GetDBInstance()
		var dbDevice models.Device
		hasher := sha256.New()
		hasher.Write([]byte(refreshToken))
		hashedToken := hex.EncodeToString(hasher.Sum(nil))

		err = db.Where("refresh_token = ? AND device_id = ?", hashedToken, deviceID).First(&dbDevice).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 缓存空值防止缓存击穿
				cacheKey := fmt.Sprintf("%s%s", cache.DeviceCachePrefix, deviceID)
				if cacheErr := cache.CacheEmptyValue(cacheKey); cacheErr != nil {
					fmt.Printf("Failed to cache empty device: %v\n", cacheErr)
				}
				return nil, nil
			}
			return nil, err
		}

		// 缓存设备信息
		if cacheErr := cache.CacheDevice(&dbDevice); cacheErr != nil {
			fmt.Printf("Failed to cache device: %v\n", cacheErr)
		}

		return &dbDevice, nil
	})

	if err != nil {
		return nil, err
	}

	return val.(*models.Device), nil
}

func DeleteRefreshToken(device *models.Device) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("device_id", device.DeviceID).Delete(device).Error
		if err != nil {
			return fmt.Errorf("failed to create device record in transaction: %w", err)
		}

		// 从缓存中删除设备信息
		if cacheErr := cache.DeleteCachedDevice(device.DeviceID); cacheErr != nil {
			fmt.Printf("Failed to delete cached device: %v\n", cacheErr)
		}

		return nil
	})
	return err
}

func SaveDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken)) // 哈希原始 token
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := dbcore.Transaction(func(tx *gorm.DB) error {
		device := &models.Device{
			UserID:       userID,
			RefreshToken: hashedToken,
			Expiry:       refreshTokenExpiry,
			DeviceID:     deviceID,
		}

		if err := tx.Create(device).Error; err != nil {
			return fmt.Errorf("failed to create device record in transaction: %w", err)
		}

		// 缓存设备信息
		if cacheErr := cache.CacheDevice(device); cacheErr != nil {
			fmt.Printf("Failed to cache device: %v\n", cacheErr)
		}

		return nil
	})

	return err
}

// RotateRefreshToken 轮换刷新令牌
func RotateRefreshToken(userID uint, deviceID, newRefreshToken string, newRefreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(newRefreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	// 删除旧的缓存
	if cacheErr := cache.DeleteCachedDevice(deviceID); cacheErr != nil {
		fmt.Printf("Failed to delete cached device: %v\n", cacheErr)
	}

	return dbcore.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error; err != nil {
			return fmt.Errorf("failed to delete old device record: %w", err)
		}

		newDevice := &models.Device{
			UserID:       userID,
			RefreshToken: hashedToken,
			Expiry:       newRefreshTokenExpiry,
			DeviceID:     deviceID,
		}
		if err := tx.Create(&newDevice).Error; err != nil {
			return fmt.Errorf("failed to create new device record: %w", err)
		}

		// 更新缓存
		if cacheErr := cache.CacheDevice(newDevice); cacheErr != nil {
			return fmt.Errorf("failed to cache device: %w", cacheErr)
		}

		return nil
	})
}

// DeleteDeviceByDeviceID 删除设备
func DeleteDeviceByDeviceID(deviceID string) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error
		if err != nil {
			return fmt.Errorf("failed to delete device record in transaction: %w", err)
		}

		// 从缓存中删除设备信息
		if cacheErr := cache.DeleteCachedDevice(deviceID); cacheErr != nil {
			fmt.Printf("Failed to delete cached device: %v\n", cacheErr)
		}

		return nil
	})

	return err
}
