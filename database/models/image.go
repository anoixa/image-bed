package models

import "gorm.io/gorm"

type Image struct {
	gorm.Model
	Identifier    string `gorm:"uniqueIndex:idx_identifier;not null"`
	OriginalName  string `gorm:"not null"`
	FileSize      int64  `gorm:"not null"`
	MimeType      string `gorm:"not null"`
	StorageDriver string `gorm:"not null"`

	FileHash string `gorm:"uniqueIndex:idx_filehash;not null"`
	Width    int
	Height   int
	IsPublic bool `gorm:"default:true;not null"`

	UserID uint `gorm:"index:idx_user_created_at,priority:1"`
	User   User `gorm:"foreignKey:UserID"`

	Albums []*Album `gorm:"many2many:album_images;"`
}
