package models

import (
	"time"

	"gorm.io/gorm"
)

// OAuthInvite represents an admin-created invite that allows an external
// identity to log in as a specific internal user.
type OAuthInvite struct {
	gorm.Model
	UserID    uint       `gorm:"not null;index" json:"user_id"`
	User      User       `gorm:"foreignKey:UserID" json:"-"`
	Provider  string     `gorm:"size:32;not null;index:idx_invite_provider_subject,priority:1;index:idx_invite_provider_email,priority:1" json:"provider"`
	Subject   string     `gorm:"size:191;index:idx_invite_provider_subject,priority:2" json:"subject,omitempty"`
	Email     string     `gorm:"size:255;index:idx_invite_provider_email,priority:2" json:"email,omitempty"`
	CreatedBy uint       `gorm:"not null;index" json:"created_by"`
	ExpiresAt *time.Time `gorm:"index" json:"expires_at,omitempty"`
	UsedAt    *time.Time `gorm:"index" json:"used_at,omitempty"`
}

// IsExpired returns true if the invite has an expiry time that has passed.
func (inv *OAuthInvite) IsExpired() bool {
	return inv.ExpiresAt != nil && inv.ExpiresAt.Before(time.Now())
}

// IsUsed returns true if the invite has already been consumed.
func (inv *OAuthInvite) IsUsed() bool {
	return inv.UsedAt != nil
}

// IsConsumable returns true if the invite can still be consumed.
func (inv *OAuthInvite) IsConsumable() bool {
	return !inv.IsUsed() && !inv.IsExpired()
}
