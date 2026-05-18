package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// DeviceRepository 设备仓库
type DeviceRepository struct {
	db *gorm.DB
}

// NewDeviceRepository 创建新的设备仓库
func NewDeviceRepository(db *gorm.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// CreateLoginDevice 创建设备登录记录
func (r *DeviceRepository) CreateLoginDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	device := &models.Device{
		UserID:       userID,
		RefreshToken: hashRefreshToken(refreshToken),
		Expiry:       refreshTokenExpiry,
		DeviceID:     deviceID,
	}
	return r.db.Create(device).Error
}

// GetDeviceByRefreshTokenAndDeviceID 通过刷新令牌和设备ID获取设备
func (r *DeviceRepository) GetDeviceByRefreshTokenAndDeviceID(refreshToken string, deviceID string) (*models.Device, error) {
	var device models.Device
	err := r.db.Where("refresh_token = ? AND device_id = ? AND expiry > ?", hashRefreshToken(refreshToken), deviceID, time.Now()).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// GetDeviceByRefreshToken 通过刷新令牌获取设备
func (r *DeviceRepository) GetDeviceByRefreshToken(refreshToken string) (*models.Device, error) {
	var device models.Device
	err := r.db.Where("refresh_token = ? AND expiry > ?", hashRefreshToken(refreshToken), time.Now()).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// RotateRefreshToken 轮换刷新令牌
func (r *DeviceRepository) RotateRefreshToken(userID uint, deviceID, newRefreshToken string, newRefreshTokenExpiry time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error; err != nil {
			return err
		}

		newDevice := &models.Device{
			UserID:       userID,
			RefreshToken: hashRefreshToken(newRefreshToken),
			Expiry:       newRefreshTokenExpiry,
			DeviceID:     deviceID,
		}
		return tx.Create(newDevice).Error
	})
}

// DeleteDeviceByDeviceID 删除设备
func (r *DeviceRepository) DeleteDeviceByDeviceID(deviceID string) error {
	return r.db.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error
}

// DeleteDeviceByDeviceIDAndRefreshToken 通过设备ID和刷新令牌删除设备（双重验证）
func (r *DeviceRepository) DeleteDeviceByDeviceIDAndRefreshToken(deviceID string, refreshToken string) error {
	result := r.db.Where("device_id = ? AND refresh_token = ?", deviceID, hashRefreshToken(refreshToken)).Delete(&models.Device{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("invalid device ID or refresh token")
	}
	return nil
}

// GetDevicesByUser 获取用户的所有设备
func (r *DeviceRepository) GetDevicesByUser(userID uint) ([]*models.Device, error) {
	var devices []*models.Device
	err := r.db.Where("user_id = ?", userID).Find(&devices).Error
	return devices, err
}

// DeleteDevicesByUser 删除用户的所有设备
func (r *DeviceRepository) DeleteDevicesByUser(userID uint) error {
	return r.db.Where("user_id = ?", userID).Delete(&models.Device{}).Error
}

// WithContext 返回带上下文的仓库
func (r *DeviceRepository) WithContext(ctx context.Context) *DeviceRepository {
	return &DeviceRepository{db: r.db.WithContext(ctx)}
}

func hashRefreshToken(token string) string {
	hasher := sha256.New()
	hasher.Write([]byte(token))
	return hex.EncodeToString(hasher.Sum(nil))
}
