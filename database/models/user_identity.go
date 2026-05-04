package models

import "gorm.io/gorm"

// UserIdentity represents a linked OAuth identity for a user.
type UserIdentity struct {
	gorm.Model
	UserID        uint   `gorm:"not null;index;uniqueIndex:idx_user_provider,priority:1" json:"user_id"`
	User          User   `gorm:"foreignKey:UserID" json:"-"`
	Provider      string `gorm:"size:32;not null;uniqueIndex:idx_provider_subject,priority:1;uniqueIndex:idx_user_provider,priority:2" json:"provider"`
	Subject       string `gorm:"size:191;not null;uniqueIndex:idx_provider_subject,priority:2" json:"subject"`
	Username      string `gorm:"size:191" json:"username,omitempty"`
	Email         string `gorm:"size:255;index" json:"email,omitempty"`
	EmailVerified bool   `gorm:"not null;default:false" json:"email_verified"`
	AvatarURL     string `gorm:"size:1024" json:"avatar_url,omitempty"`
}
