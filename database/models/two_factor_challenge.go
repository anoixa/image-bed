package models

import "time"

// TwoFactorChallenge is a short-lived, single-use login challenge.
// The raw ticket is only returned to the client; the database stores its hash.
type TwoFactorChallenge struct {
	ID         uint `gorm:"primarykey"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	TicketHash string    `gorm:"uniqueIndex;size:64;not null"`
	UserID     uint      `gorm:"index;not null"`
	Attempts   int       `gorm:"not null;default:0"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	ConsumedAt *time.Time
}
