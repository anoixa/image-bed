package models

import "gorm.io/gorm"

// 角色常量定义
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// 用户状态常量
const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type User struct {
	gorm.Model
	Username string `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Password string `gorm:"size:255;not null" json:"-"`
	Role     string `gorm:"size:32;default:'user'" json:"role"`     // 用户角色：admin, user
	Status   string `gorm:"size:32;default:'active'" json:"status"` // 用户状态：active, disabled
}

// IsActive checks if user is active
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

// IsAdmin 检查用户是否为管理员
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}
