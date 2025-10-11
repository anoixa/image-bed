package models

import (
	"time"

	"gorm.io/gorm"
)

type Device struct {
	gorm.Model
	UserID       uint      `gorm:"index;not null"` // 显式为 UserID 添加索引，用于查询用户的所有设备
	User         User      `gorm:"foreignKey:UserID"`
	RefreshToken string    `gorm:"uniqueIndex;not null"`
	DeviceID     string    `gorm:"uniqueIndex:idx_active_device,where:deleted_at IS NULL"`
	Expiry       time.Time `gorm:"not null"`
}
