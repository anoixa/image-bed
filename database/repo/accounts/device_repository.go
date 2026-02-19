package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// DeviceRepository 设备仓库 - 封装所有设备相关的数据库操作
type DeviceRepository struct {
	db database.Provider
}

// NewDeviceRepository 创建新的设备仓库
func NewDeviceRepository(db database.Provider) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// CreateLoginDevice 创建设备登录记录
func (r *DeviceRepository) CreateLoginDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken)) // 哈希原始 token
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := r.db.Transaction(func(tx *gorm.DB) error {
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

// GetDeviceByRefreshTokenAndDeviceID 通过刷新令牌和设备ID获取设备
func (r *DeviceRepository) GetDeviceByRefreshTokenAndDeviceID(refreshToken string, deviceID string) (*models.Device, error) {
	var device models.Device

	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := r.db.DB().Where("refresh_token = ? AND device_id = ? AND expiry > ?", hashedToken, deviceID, time.Now()).First(&device).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &device, nil
}

// DeleteRefreshToken 删除刷新令牌
func (r *DeviceRepository) DeleteRefreshToken(device *models.Device) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("device_id", device.DeviceID).Delete(device).Error
		if err != nil {
			return fmt.Errorf("failed to delete device record in transaction: %w", err)
		}

		return nil
	})
	return err
}

// SaveDevice 保存设备
func (r *DeviceRepository) SaveDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken)) // 哈希原始 token
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	err := r.db.Transaction(func(tx *gorm.DB) error {
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
func (r *DeviceRepository) RotateRefreshToken(userID uint, deviceID, newRefreshToken string, newRefreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(newRefreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	return r.db.Transaction(func(tx *gorm.DB) error {
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
func (r *DeviceRepository) DeleteDeviceByDeviceID(deviceID string) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error
		if err != nil {
			return fmt.Errorf("failed to delete device record in transaction: %w", err)
		}

		return nil
	})

	return err
}

// GetDevicesByUser 获取用户的所有设备
func (r *DeviceRepository) GetDevicesByUser(userID uint) ([]*models.Device, error) {
	var devices []*models.Device
	err := r.db.DB().Where("user_id = ?", userID).Find(&devices).Error
	return devices, err
}

// DeleteDevicesByUser 删除用户的所有设备
func (r *DeviceRepository) DeleteDevicesByUser(userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Where("user_id = ?", userID).Delete(&models.Device{}).Error
	})
}

// CountDevicesByUser 统计用户的设备数量
func (r *DeviceRepository) CountDevicesByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.DB().Model(&models.Device{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// WithContext 返回带上下文的仓库
func (r *DeviceRepository) WithContext(ctx context.Context) *DeviceRepository {
	return &DeviceRepository{db: &contextProvider{Provider: r.db, ctx: ctx}}
}
