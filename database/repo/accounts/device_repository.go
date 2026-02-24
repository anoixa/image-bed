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

// DeviceRepository 设备仓库 - 封装所有设备相关的数据库操作
type DeviceRepository struct {
	db *gorm.DB
}

// NewDeviceRepository 创建新的设备仓库
func NewDeviceRepository(db *gorm.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// CreateLoginDevice 创建设备登录记录
func (r *DeviceRepository) CreateLoginDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	device := &models.Device{
		UserID:       userID,
		RefreshToken: hashedToken,
		Expiry:       refreshTokenExpiry,
		DeviceID:     deviceID,
	}
	return r.db.Create(device).Error
}

// GetDeviceByRefreshTokenAndDeviceID 通过刷新令牌和设备ID获取设备
func (r *DeviceRepository) GetDeviceByRefreshTokenAndDeviceID(refreshToken string, deviceID string) (*models.Device, error) {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	var device models.Device
	err := r.db.Where("refresh_token = ? AND device_id = ? AND expiry > ?", hashedToken, deviceID, time.Now()).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// DeleteRefreshToken 删除刷新令牌
func (r *DeviceRepository) DeleteRefreshToken(device *models.Device) error {
	return r.db.Where("device_id", device.DeviceID).Delete(device).Error
}

// SaveDevice 保存设备
func (r *DeviceRepository) SaveDevice(userID uint, deviceID string, refreshToken string, refreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	device := &models.Device{
		UserID:       userID,
		RefreshToken: hashedToken,
		Expiry:       refreshTokenExpiry,
		DeviceID:     deviceID,
	}
	return r.db.Create(device).Error
}

// RotateRefreshToken 轮换刷新令牌
func (r *DeviceRepository) RotateRefreshToken(userID uint, deviceID, newRefreshToken string, newRefreshTokenExpiry time.Time) error {
	hasher := sha256.New()
	hasher.Write([]byte(newRefreshToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("device_id = ?", deviceID).Delete(&models.Device{}).Error; err != nil {
			return err
		}

		newDevice := &models.Device{
			UserID:       userID,
			RefreshToken: hashedToken,
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

// CountDevicesByUser 统计用户的设备数量
func (r *DeviceRepository) CountDevicesByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.Device{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// WithContext 返回带上下文的仓库
func (r *DeviceRepository) WithContext(ctx context.Context) *DeviceRepository {
	return &DeviceRepository{db: r.db.WithContext(ctx)}
}
