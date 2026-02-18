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
	Identifier   string         `gorm:"not null;size:255" json:"identifier"`                           // 存储标识
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

// 状态常量
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

// 默认缩略图尺寸
var DefaultThumbnailSizes = []ThumbnailSize{
	{Name: "small", Width: 150, Height: 0},
	{Name: "medium", Width: 300, Height: 0},
	{Name: "large", Width: 600, Height: 0},
}

// GetThumbnailFormat 获取指定尺寸的缩略图格式标识
func GetThumbnailFormat(width int) string {
	return fmt.Sprintf("%s_%d", FormatThumbnail, width)
}

// ParseThumbnailWidth 从格式标识解析缩略图宽度
func ParseThumbnailWidth(format string) (int, bool) {
	if !IsThumbnailFormat(format) {
		return 0, false
	}
	var width int
	_, err := fmt.Sscanf(format, FormatThumbnail+"_%d", &width)
	if err != nil {
		return 0, false
	}
	return width, true
}

// IsThumbnailFormat 检查是否为缩略图格式
func IsThumbnailFormat(format string) bool {
	return len(format) > len(FormatThumbnail) &&
		format[:len(FormatThumbnail)+1] == FormatThumbnail+"_"
}

// IsValidThumbnailWidth 检查是否为有效的缩略图宽度
func IsValidThumbnailWidth(width int, allowedSizes []ThumbnailSize) bool {
	for _, size := range allowedSizes {
		if size.Width == width {
			return true
		}
	}
	return false
}
