package configs

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// DefaultCacheTTL 默认缓存过期时间
const DefaultCacheTTL = 5 * time.Minute

// Repository 配置仓库接口
type Repository interface {
	Create(ctx context.Context, config *models.SystemConfig) error
	Update(ctx context.Context, config *models.SystemConfig) error
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*models.SystemConfig, error)
	GetByKey(ctx context.Context, key string) (*models.SystemConfig, error)
	List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error)
	ListAll(ctx context.Context) ([]models.SystemConfig, error)
	GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error)
	SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error
	Enable(ctx context.Context, id uint) error
	Disable(ctx context.Context, id uint) error
	Count(ctx context.Context) (int64, error)
	CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error)
	Exists(ctx context.Context, key string) (bool, error)
	Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
	EnsureKeyUnique(ctx context.Context, baseKey string) (string, error)
}

// ConfigRepository 配置仓库实现
type ConfigRepository struct {
	db *gorm.DB
}

// NewRepository 创建配置仓库
func NewRepository(db *gorm.DB) *ConfigRepository {
	return &ConfigRepository{db: db}
}

// Create 创建配置
func (r *ConfigRepository) Create(ctx context.Context, config *models.SystemConfig) error {
	return r.db.WithContext(ctx).Create(config).Error
}

// Update 更新配置
func (r *ConfigRepository) Update(ctx context.Context, config *models.SystemConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

// Delete 删除配置
func (r *ConfigRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.SystemConfig{}, id).Error
}

// GetByID 根据ID获取配置
func (r *ConfigRepository) GetByID(ctx context.Context, id uint) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).First(&config, id).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// GetByKey 根据Key获取配置
func (r *ConfigRepository) GetByKey(ctx context.Context, key string) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// List 列出配置（支持分类过滤）
func (r *ConfigRepository) List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error) {
	var configs []models.SystemConfig
	query := r.db.WithContext(ctx).Order("priority DESC, id ASC")

	if category != "" {
		query = query.Where("category = ?", category)
	}
	if enabledOnly {
		query = query.Where("is_enabled = ?", true)
	}

	if err := query.Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// ListAll 列出所有配置
func (r *ConfigRepository) ListAll(ctx context.Context) ([]models.SystemConfig, error) {
	return r.List(ctx, "", false)
}

// GetDefaultByCategory 获取默认配置
func (r *ConfigRepository) GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).
		Where("category = ? AND is_default = ? AND is_enabled = ?", category, true, true).
		First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// SetDefault 设置默认配置
func (r *ConfigRepository) SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.SystemConfig{}).Where("category = ?", category).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&models.SystemConfig{}).Where("id = ?", id).Update("is_default", true).Error
	})
}

// Enable 启用配置
func (r *ConfigRepository) Enable(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&models.SystemConfig{}).Where("id = ?", id).Update("is_enabled", true).Error
}

// Disable 禁用配置
func (r *ConfigRepository) Disable(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&models.SystemConfig{}).Where("id = ?", id).Update("is_enabled", false).Error
}

// Count 统计配置数量
func (r *ConfigRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.SystemConfig{}).Count(&count).Error
	return count, err
}

// CountByCategory 按分类统计
func (r *ConfigRepository) CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.SystemConfig{}).Where("category = ?", category).Count(&count).Error
	return count, err
}

// Exists 检查Key是否已存在
func (r *ConfigRepository) Exists(ctx context.Context, key string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.SystemConfig{}).Where("key = ?", key).Count(&count).Error
	return count > 0, err
}

// Transaction 事务支持
func (r *ConfigRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// EnsureKeyUnique 确保 Key 唯一，如果不唯一则添加序号
func (r *ConfigRepository) EnsureKeyUnique(ctx context.Context, baseKey string) (string, error) {
	key := baseKey
	for i := 1; i < 1000; i++ {
		exists, err := r.Exists(ctx, key)
		if err != nil {
			return "", err
		}
		if !exists {
			return key, nil
		}
		key = fmt.Sprintf("%s_%d", baseKey, i)
	}
	return "", fmt.Errorf("failed to generate unique key for %s", baseKey)
}
