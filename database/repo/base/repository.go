// Package base 提供通用的 Repository 基类
package base

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Repository 通用仓库基类
type Repository[T any] struct {
	db *gorm.DB
}

// NewRepository 创建新的通用仓库
func NewRepository[T any](db *gorm.DB) *Repository[T] {
	return &Repository[T]{db: db}
}

// DB 返回底层数据库连接
func (r *Repository[T]) DB() *gorm.DB {
	return r.db
}

// Create 创建记录
func (r *Repository[T]) Create(ctx context.Context, entity *T) error {
	return r.db.WithContext(ctx).Create(entity).Error
}

// CreateWithTx 在事务中创建记录
func (r *Repository[T]) CreateWithTx(tx *gorm.DB, entity *T) error {
	return tx.Create(entity).Error
}

// GetByID 通过 ID 获取记录
func (r *Repository[T]) GetByID(ctx context.Context, id uint) (*T, error) {
	var entity T
	err := r.db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// Update 更新记录
func (r *Repository[T]) Update(ctx context.Context, entity *T) error {
	return r.db.WithContext(ctx).Save(entity).Error
}

// UpdateWithTx 在事务中更新记录
func (r *Repository[T]) UpdateWithTx(tx *gorm.DB, entity *T) error {
	return tx.Save(entity).Error
}

// Delete 删除记录
func (r *Repository[T]) Delete(ctx context.Context, id uint) error {
	var entity T
	return r.db.WithContext(ctx).Delete(&entity, id).Error
}

// DeleteByIDs 批量删除记录
func (r *Repository[T]) DeleteByIDs(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var entity T
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&entity).Error
}

// List 获取记录列表（支持分页）
func (r *Repository[T]) List(ctx context.Context, page, pageSize int) ([]*T, int64, error) {
	var entities []*T
	var total int64

	db := r.db.WithContext(ctx).Model(new(T))
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&entities).Error
	return entities, total, err
}

// Count 获取记录总数
func (r *Repository[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(new(T)).Count(&count).Error
	return count, err
}

// Exists 检查记录是否存在
func (r *Repository[T]) Exists(ctx context.Context, id uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(new(T)).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

// FindByCondition 根据条件查询
func (r *Repository[T]) FindByCondition(ctx context.Context, condition string, args ...interface{}) ([]*T, error) {
	var entities []*T
	err := r.db.WithContext(ctx).Where(condition, args...).Find(&entities).Error
	return entities, err
}

// FirstByCondition 根据条件查询第一条记录
func (r *Repository[T]) FirstByCondition(ctx context.Context, condition string, args ...interface{}) (*T, error) {
	var entity T
	err := r.db.WithContext(ctx).Where(condition, args...).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// Transaction 执行事务
func (r *Repository[T]) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}
