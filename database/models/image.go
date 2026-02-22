package models

import "gorm.io/gorm"

// 图片变体状态常量
type ImageVariantStatus int8

const (
	ImageVariantStatusNone               ImageVariantStatus = 0 // 无需变体（默认）
	ImageVariantStatusProcessing         ImageVariantStatus = 1 // 有变体正在处理中
	ImageVariantStatusThumbnailCompleted ImageVariantStatus = 2 // 缩略图已完成，WebP 未完成（缩略图优先级高）
	ImageVariantStatusCompleted          ImageVariantStatus = 3 // 所有变体（缩略图+WebP）都已完成
	ImageVariantStatusFailed             ImageVariantStatus = 4 // 变体处理失败/部分失败
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
	gorm.Model
	Identifier      string `gorm:"uniqueIndex:idx_identifier;not null"`
	OriginalName    string `gorm:"not null"`
	FileSize        int64  `gorm:"not null"`
	MimeType        string `gorm:"not null"`
	StorageConfigID uint   `gorm:"column:storage_config_id;not null"`

	FileHash string `gorm:"uniqueIndex:idx_filehash;not null"`
	Width    int
	Height   int
	IsPublic bool `gorm:"default:true;not null"`

	VariantStatus ImageVariantStatus `gorm:"default:0;not null"`

	UserID uint `gorm:"index:idx_user_created_at,priority:1"`
	User   User `gorm:"foreignKey:UserID"`

	Albums []*Album `gorm:"many2many:album_images;"`

	IsPendingDeletion bool `gorm:"default:false;not null" json:"-"`
}
