package models

import (
	"time"

	"gorm.io/gorm"
)

type ApiToken struct {
	gorm.Model
	ID          uint       `gorm:"primaryKey" json:"id"`
	UserID      uint       `gorm:"index:idx_user_active;not null" json:"user_id"`
	IsActive    bool       `gorm:"index:idx_user_active" json:"isActive"`
	Token       string     `gorm:"size:64;unique;not null"`
	Description string     `gorm:"size:255"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`

	User User `gorm:"foreignKey:UserID"`
}
