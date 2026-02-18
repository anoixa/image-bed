package configs

import (
	"context"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Repository 配置仓库接口
type Repository interface {
	// Create 创建配置
	Create(ctx context.Context, config *models.SystemConfig) error
	// Update 更新配置
	Update(ctx context.Context, config *models.SystemConfig) error
	// Delete 删除配置
	Delete(ctx context.Context, id uint) error
	// GetByID 根据ID获取配置
	GetByID(ctx context.Context, id uint) (*models.SystemConfig, error)
	// GetByKey 根据Key获取配置
	GetByKey(ctx context.Context, key string) (*models.SystemConfig, error)
	// List 列出配置（支持分类过滤）
	List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error)
	// ListAll 列出所有配置
	ListAll(ctx context.Context) ([]models.SystemConfig, error)
	// GetDefaultByCategory 获取默认配置
	GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error)
	// SetDefault 设置默认配置
	SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error
	// Enable 启用配置
	Enable(ctx context.Context, id uint) error
	// Disable 禁用配置
	Disable(ctx context.Context, id uint) error
	// Count 统计配置数量（用于检查数据库是否已有数据）
	Count(ctx context.Context) (int64, error)
	// CountByCategory 按分类统计
	CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error)
	// Exists 检查Key是否已存在
	Exists(ctx context.Context, key string) (bool, error)
	// Transaction 事务支持
	Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
	// EnsureKeyUnique 确保Key唯一，如果不唯一则添加序号
	EnsureKeyUnique(ctx context.Context, baseKey string) (string, error)
}

// repository 配置仓库实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建配置仓库
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create 创建配置
func (r *repository) Create(ctx context.Context, config *models.SystemConfig) error {
	return r.db.WithContext(ctx).Create(config).Error
}

// Update 更新配置
func (r *repository) Update(ctx context.Context, config *models.SystemConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

// Delete 删除配置
func (r *repository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.SystemConfig{}, id).Error
}

// GetByID 根据ID获取配置
func (r *repository) GetByID(ctx context.Context, id uint) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).First(&config, id).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// GetByKey 根据Key获取配置
func (r *repository) GetByKey(ctx context.Context, key string) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// List 列出配置（支持分类过滤）
func (r *repository) List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error) {
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
func (r *repository) ListAll(ctx context.Context) ([]models.SystemConfig, error) {
	return r.List(ctx, "", false)
}

// GetDefaultByCategory 获取默认配置
func (r *repository) GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error) {
	var config models.SystemConfig
	if err := r.db.WithContext(ctx).
		Where("category = ? AND is_default = ? AND is_enabled = ?", category, true, true).
		First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// SetDefault 设置默认配置
func (r *repository) SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先将同分类的所有配置设为非默认
		if err := tx.Model(&models.SystemConfig{}).
			Where("category = ?", category).
			Update("is_default", false).Error; err != nil {
			return err
		}

		// 再将指定配置设为默认
		return tx.Model(&models.SystemConfig{}).
			Where("id = ?", id).
			Update("is_default", true).Error
	})
}

// Enable 启用配置
func (r *repository) Enable(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&models.SystemConfig{}).
		Where("id = ?", id).
		Update("is_enabled", true).Error
}

// Disable 禁用配置
func (r *repository) Disable(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&models.SystemConfig{}).
		Where("id = ?", id).
		Update("is_enabled", false).Error
}

// Count 统计配置数量（用于检查数据库是否已有数据）
func (r *repository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.SystemConfig{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountByCategory 按分类统计
func (r *repository) CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&models.SystemConfig{}).
		Where("category = ?", category).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Exists 检查Key是否已存在
func (r *repository) Exists(ctx context.Context, key string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&models.SystemConfig{}).
		Where("key = ?", key).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// Transaction 事务支持
func (r *repository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// GormDB 获取原始 GORM DB 实例（用于复杂查询）
func (r *repository) GormDB() *gorm.DB {
	return r.db
}

// EnsureKeyUnique 确保 Key 唯一，如果不唯一则添加序号
func (r *repository) EnsureKeyUnique(ctx context.Context, baseKey string) (string, error) {
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
