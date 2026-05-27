package models

import (
	"time"

	"gorm.io/gorm"
)

type ImageVariantStatus int8

const (
	ImageVariantStatusNone               ImageVariantStatus = 0 // 无需变体（默认）
	ImageVariantStatusProcessing         ImageVariantStatus = 1 // 有变体正在处理中
	ImageVariantStatusThumbnailCompleted ImageVariantStatus = 2 // 缩略图已完成，WebP 未完成（缩略图优先级高）
	ImageVariantStatusCompleted          ImageVariantStatus = 3 // 所有变体（缩略图+WebP）都已完成
	ImageVariantStatusFailed             ImageVariantStatus = 4
)

// IsVariantAvailable 检查变体是否可用（已完成状态）
func (s ImageVariantStatus) IsVariantAvailable() bool {
	return s == ImageVariantStatusThumbnailCompleted || s == ImageVariantStatusCompleted
}

// HasPendingVariants 检查是否还有待处理的变体
func (s ImageVariantStatus) HasPendingVariants() bool {
	return s == ImageVariantStatusNone || s == ImageVariantStatusThumbnailCompleted
}

type Image struct {
	ID        uint `gorm:"primarykey;index:idx_images_public_id,priority:2"`
	CreatedAt time.Time
	UpdatedAt time.Time      `gorm:"index:idx_image_variant_status_updated_at,priority:2"`
	DeletedAt gorm.DeletedAt `gorm:"uniqueIndex:idx_filehash_deleted;index"`

	Identifier      string `gorm:"not null"`
	StoragePath     string `gorm:"not null"`
	OriginalName    string `gorm:"not null"`
	FileSize        int64  `gorm:"not null;index:idx_images_public_file_size,priority:2"`
	MimeType        string `gorm:"not null"`
	StorageConfigID uint   `gorm:"column:storage_config_id;not null"`

	FileHash string `gorm:"uniqueIndex:idx_filehash_deleted;not null"`
	Width    int    `gorm:"index:idx_images_public_width,priority:2"`
	Height   int    `gorm:"index:idx_images_public_height,priority:2"`
	IsPublic bool   `gorm:"default:true;not null;index:idx_images_public_id,priority:1;index:idx_images_public_variant,priority:1;index:idx_images_public_file_size,priority:1;index:idx_images_public_width,priority:1;index:idx_images_public_height,priority:1"`

	VariantStatus ImageVariantStatus `gorm:"default:0;not null;index:idx_image_variant_status_updated_at,priority:1;index:idx_images_public_variant,priority:2"`

	UserID uint `gorm:"index:idx_user_created_at,priority:1"`
	User   User `gorm:"foreignKey:UserID"`

	Albums []*Album `gorm:"many2many:album_images;"`

	IsPendingDeletion bool `gorm:"default:false;not null" json:"-"`
}
