package models

import (
	"gorm.io/gorm"
	"time"
)

type Image struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	FileName    string         `gorm:"not null" json:"file_name"`
	FileSize    int64          `json:"file_size"`
	MimeType    string         `json:"mime_type"`
	StoragePath string         `json:"storage_path"`
	UploadTime  time.Time      `json:"upload_time"`
}