package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 通用仓库接口
type Repository[T any] interface {
	Create(ctx context.Context, entity *T) error
	GetByID(ctx context.Context, id uint) (*T, error)
	Update(ctx context.Context, entity *T) error
	Delete(ctx context.Context, id uint) error
	List(ctx context.Context, page, limit int) ([]*T, int64, error)
	Count(ctx context.Context) (int64, error)
}

// GenericRepository 通用仓库实现
type GenericRepository[T any] struct {
	db *gorm.DB
}

// NewGenericRepository 创建通用仓库
func NewGenericRepository[T any](db *gorm.DB) *GenericRepository[T] {
	return &GenericRepository[T]{db: db}
}

// Create 创建实体
func (r *GenericRepository[T]) Create(ctx context.Context, entity *T) error {
	return r.db.WithContext(ctx).Create(entity).Error
}

// GetByID 根据 ID 获取实体
func (r *GenericRepository[T]) GetByID(ctx context.Context, id uint) (*T, error) {
	var entity T
	err := r.db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// Update 更新实体
func (r *GenericRepository[T]) Update(ctx context.Context, entity *T) error {
	return r.db.WithContext(ctx).Save(entity).Error
}

// Delete 根据 ID 删除实体
func (r *GenericRepository[T]) Delete(ctx context.Context, id uint) error {
	var entity T
	return r.db.WithContext(ctx).Delete(&entity, id).Error
}

// List 分页获取列表
func (r *GenericRepository[T]) List(ctx context.Context, page, limit int) ([]*T, int64, error) {
	var entities []*T
	var total int64

	db := r.db.WithContext(ctx).Model(new(T))
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := db.Offset(offset).Limit(limit).Find(&entities).Error
	return entities, total, err
}

// Count 获取总数
func (r *GenericRepository[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(new(T)).Count(&count).Error
	return count, err
}

// DB 获取底层 *gorm.DB
func (r *GenericRepository[T]) DB() *gorm.DB {
	return r.db
}

// WithContext 返回带上下文的仓库
func (r *GenericRepository[T]) WithContext(ctx context.Context) *GenericRepository[T] {
	return &GenericRepository[T]{db: r.db.WithContext(ctx)}
}

// Transaction 执行事务
func (r *GenericRepository[T]) Transaction(fn func(*gorm.DB) error) error {
	return r.db.Transaction(fn)
}

// FindBy 根据条件查询（单个结果）
func (r *GenericRepository[T]) FindBy(ctx context.Context, query string, args ...any) (*T, error) {
	var entity T
	err := r.db.WithContext(ctx).Where(query, args...).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// FindAllBy 根据条件查询（多个结果）
func (r *GenericRepository[T]) FindAllBy(ctx context.Context, query string, args ...any) ([]*T, error) {
	var entities []*T
	err := r.db.WithContext(ctx).Where(query, args...).Find(&entities).Error
	return entities, err
}

// Exists 根据条件检查是否存在
func (r *GenericRepository[T]) Exists(ctx context.Context, query string, args ...any) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(new(T)).Where(query, args...).Count(&count).Error
	return count > 0, err
}

// BatchCreate 批量创建
func (r *GenericRepository[T]) BatchCreate(ctx context.Context, entities []*T) error {
	if len(entities) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(entities, 100).Error
}

// BatchDelete 批量删除
func (r *GenericRepository[T]) BatchDelete(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var entity T
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&entity).Error
}

// GetByIDWithLock 根据 ID 获取实体并加锁
func (r *GenericRepository[T]) GetByIDWithLock(ctx context.Context, id uint) (*T, error) {
	var entity T
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&entity, id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// SoftDelete 软删除
func (r *GenericRepository[T]) SoftDelete(ctx context.Context, id uint) error {
	var entity T
	result := r.db.WithContext(ctx).Delete(&entity, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("record not found")
	}
	return nil
}
