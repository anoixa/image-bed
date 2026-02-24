package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ImageVariant 图片格式变体
type ImageVariant struct {
	ID           uint           `gorm:"primarykey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	ImageID      uint           `gorm:"not null;index:idx_image_format,unique" json:"image_id"`
	Format       string         `gorm:"not null;size:20;index:idx_image_format,unique" json:"format"` // webp, avif, thumbnail_150
	Identifier   string         `gorm:"not null;size:255" json:"identifier"`                          // 业务标识符: a1b2c3d4e5f6_300.webp
	StoragePath  string         `gorm:"not null;size:255" json:"storage_path"`                        // 存储路径: thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp
	FileSize     int64          `gorm:"not null" json:"file_size"`
	Width        int            `json:"width"`
	Height       int            `json:"height"`
	Status       string         `gorm:"default:pending;size:20;index" json:"status"`
	ErrorMessage string         `gorm:"type:text" json:"error_message,omitempty"`
	RetryCount   int            `gorm:"default:0" json:"retry_count"`
	NextRetryAt  *time.Time     `gorm:"index" json:"next_retry_at,omitempty"`
}

// TableName 指定表名
func (ImageVariant) TableName() string {
	return "image_variants"
}

const (
	VariantStatusPending    = "pending"
	VariantStatusProcessing = "processing"
	VariantStatusCompleted  = "completed"
	VariantStatusFailed     = "failed"
)

// Format 常量
const (
	FormatWebP      = "webp"
	FormatAVIF      = "avif"
	FormatThumbnail = "thumbnail"
)

// ThumbnailSize 缩略图尺寸配置
type ThumbnailSize struct {
	Name   string
	Width  int
	Height int // 0 表示保持比例
}

// DefaultThumbnailSizes 默认缩略图尺寸
var DefaultThumbnailSizes = []ThumbnailSize{
	{Name: "small", Width: 150, Height: 0},
	{Name: "medium", Width: 300, Height: 0},
	{Name: "large", Width: 600, Height: 0},
}

// FormatThumbnailSize 生成缩略图格式标识
func FormatThumbnailSize(width int) string {
	return fmt.Sprintf("thumbnail_%d", width)
}

// ParseThumbnailSize 从格式标识解析缩略图尺寸
func ParseThumbnailSize(format string) (width int, ok bool) {
	if _, err := fmt.Sscanf(format, "thumbnail_%d", &width); err == nil {
		return width, true
	}
	return 0, false
}
