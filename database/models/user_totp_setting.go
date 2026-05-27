package models

import "time"

// UserTOTPSetting stores a user's TOTP configuration outside the users table.
// Secrets are encrypted before persistence.
type UserTOTPSetting struct {
	ID                     uint `gorm:"primarykey"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
	UserID                 uint   `gorm:"uniqueIndex;not null"`
	SecretEncrypted        string `gorm:"type:text"`
	PendingSecretEncrypted string `gorm:"type:text"`
	PendingExpiresAt       *time.Time
	Enabled                bool `gorm:"not null;default:false"`
	EnabledAt              *time.Time
	LastUsedStep           int64 `gorm:"not null;default:0"`
}
