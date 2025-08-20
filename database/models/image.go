package models

import "gorm.io/gorm"

type Image struct {
	gorm.Model
	Identifier    string `gorm:"unique;not null"`
	OriginalName  string `gorm:"not null"`
	FileSize      int64  `gorm:"not null"`
	MimeType      string `gorm:"not null"`
	StorageDriver string `gorm:"not null"`

	FileHash string `gorm:"unique;not null"`
	Width    int
	Height   int

	UserID uint
	User   User `gorm:"foreignKey:UserID"`
}
