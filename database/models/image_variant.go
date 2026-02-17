package models

import (
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
	Format       string         `gorm:"not null;size:10;index:idx_image_format,unique" json:"format"` // webp, avif
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
	FormatWebP = "webp"
	FormatAVIF = "avif"
)
