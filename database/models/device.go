package models

import (
	"time"

	"gorm.io/gorm"
)

type Device struct {
	gorm.Model
	UserID       uint      `gorm:"index;not null"`
	User         User      `gorm:"foreignKey:UserID"`
	RefreshToken string    `gorm:"uniqueIndex;not null"`
	DeviceID     string    `gorm:"uniqueIndex:idx_active_device,where:deleted_at IS NULL"`
	Expiry       time.Time `gorm:"not null"`
}
