package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"gorm.io/gorm"
	"image-bed/database/dbcore"
	"image-bed/database/models"
	"time"
)

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
		return nil
	})
	return err
}

// GetDeviceByRefreshTokenAndDeviceID Get device by refresh token and device id
func GetDeviceByRefreshTokenAndDeviceID(refreshToken string, deviceID string) (*models.Device, error) {
	db := dbcore.GetDBInstance()
	var device models.Device
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := db.Where("refresh_token = ? AND device_id = ?", hashedToken, deviceID).First(&device).Error
	if err != nil {
		return nil, err
	}

	return &device, nil
}

func DeleteRefreshToken(device *models.Device) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("device_id", device.DeviceID).Delete(device).Error
		if err != nil {
			return fmt.Errorf("failed to create device record in transaction: %w", err)
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
		return nil
	})

	return err
}

// RotateRefreshToken 轮换刷新令牌
func RotateRefreshToken(userID uint, deviceID, newRefreshToken string, newRefreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(newRefreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

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
		return nil
	})

	return err
}
