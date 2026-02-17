package models

import (
	"time"

	"gorm.io/gorm"
)

// ConfigCategory 配置分类
type ConfigCategory string

const (
	// ConfigCategoryStorage 存储配置
	ConfigCategoryStorage ConfigCategory = "storage"
	// ConfigCategoryCache 缓存配置
	ConfigCategoryCache ConfigCategory = "cache"
	// ConfigCategoryJWT JWT配置
	ConfigCategoryJWT ConfigCategory = "jwt"
	// ConfigCategoryUpload 上传配置
	ConfigCategoryUpload ConfigCategory = "upload"
	// ConfigCategoryServer 服务器配置
	ConfigCategoryServer ConfigCategory = "server"
	// ConfigCategoryRateLimit 限流配置
	ConfigCategoryRateLimit ConfigCategory = "rate_limit"
	// ConfigCategorySystem 系统内部配置
	ConfigCategorySystem ConfigCategory = "system"
	// ConfigCategoryConversion 格式转换配置
	ConfigCategoryConversion ConfigCategory = "conversion"
)

// SystemConfig 通用系统配置表
type SystemConfig struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Category ConfigCategory `gorm:"index:idx_category_enabled;not null" json:"category"`
	Name     string         `gorm:"not null" json:"name"`
	Key      string         `gorm:"uniqueIndex;not null" json:"key"` // 唯一标识，如 "storage:minio:1"

	IsEnabled bool `gorm:"default:true;index:idx_category_enabled" json:"is_enabled"`
	IsDefault bool `gorm:"default:false" json:"is_default"`
	Priority  int  `gorm:"default:0" json:"priority"`

	// ConfigJSON 使用 type:text 保底，Postgres -> jsonb
	ConfigJSON string `gorm:"type:text;not null" json:"-"` // 加密

	Description string `json:"description"`
	CreatedBy   uint   `json:"created_by"`
}

// TableName 指定表名
func (SystemConfig) TableName() string {
	return "system_configs"
}

// ToResponse 转换为响应结构（脱敏）
func (sc *SystemConfig) ToResponse(config map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          sc.ID,
		"category":    sc.Category,
		"name":        sc.Name,
		"key":         sc.Key,
		"is_enabled":  sc.IsEnabled,
		"is_default":  sc.IsDefault,
		"priority":    sc.Priority,
		"config":      config,
		"description": sc.Description,
		"created_by":  sc.CreatedBy,
		"created_at":  sc.CreatedAt,
		"updated_at":  sc.UpdatedAt,
	}
}

// ConfigResponse 配置响应结构
type ConfigResponse struct {
	ID          uint                   `json:"id"`
	Category    ConfigCategory         `json:"category"`
	Name        string                 `json:"name"`
	Key         string                 `json:"key"`
	IsEnabled   bool                   `json:"is_enabled"`
	IsDefault   bool                   `json:"is_default"`
	Priority    int                    `json:"priority"`
	Config      map[string]interface{} `json:"config"`
	Description string                 `json:"description"`
	CreatedBy   uint                   `json:"created_by"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// SystemConfigStoreRequest 创建/更新配置请求
type SystemConfigStoreRequest struct {
	Category    ConfigCategory         `json:"category" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Config      map[string]interface{} `json:"config" binding:"required"`
	IsEnabled   *bool                  `json:"is_enabled,omitempty"`
	IsDefault   *bool                  `json:"is_default,omitempty"`
	Priority    *int                   `json:"priority,omitempty"`
	Description string                 `json:"description"`
}

// TestConfigRequest 测试配置请求
type TestConfigRequest struct {
	Category ConfigCategory         `json:"category" binding:"required"`
	Config   map[string]interface{} `json:"config" binding:"required"`
}

// TestConfigResponse 测试配置响应
type TestConfigResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
