package models

import "gorm.io/gorm"

type Album struct {
	gorm.Model
	UserID      uint   `gorm:"not null;index"`
	Name        string `gorm:"type:varchar(100);not null;index"`
	Description string `gorm:"type:varchar(255)"`

	Images []*Image `gorm:"many2many:album_images;"`
}
