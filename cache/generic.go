package cache

import (
	"context"
	"fmt"
	"time"
)

// Cacheable 可缓存实体接口
type Cacheable interface {
	CacheKey() string
}

// CacheEntity 泛型缓存实体
func CacheEntity[T any](h *Helper, ctx context.Context, key string, entity *T, ttl time.Duration) error {
	if h.provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}
	return h.provider.Set(ctx, key, entity, addJitter(ttl))
}

// GetCachedEntity 泛型获取缓存实体
func GetCachedEntity[T any](h *Helper, ctx context.Context, key string, dest *T) error {
	if h.provider == nil {
		return ErrCacheMiss
	}

	if isEmpty, err := h.IsEmptyValue(ctx, key); err == nil && isEmpty {
		return ErrCacheMiss
	}

	return h.provider.Get(ctx, key, dest)
}

// DeleteCachedEntity 泛型删除缓存
func DeleteCachedEntity(h *Helper, ctx context.Context, key string) error {
	if h.provider == nil {
		return nil
	}
	return h.provider.Delete(ctx, key)
}

// CacheWithID 使用 ID 缓存实体
func CacheWithID[T any](h *Helper, ctx context.Context, prefix string, id uint, entity *T, ttl time.Duration) error {
	key := fmt.Sprintf("%s%d", prefix, id)
	return CacheEntity(h, ctx, key, entity, ttl)
}

// GetCachedWithID 使用 ID 获取缓存
func GetCachedWithID[T any](h *Helper, ctx context.Context, prefix string, id uint, dest *T) error {
	key := fmt.Sprintf("%s%d", prefix, id)
	return GetCachedEntity(h, ctx, key, dest)
}

// DeleteCachedWithID 使用 ID 删除缓存
func DeleteCachedWithID(h *Helper, ctx context.Context, prefix string, id uint) error {
	key := fmt.Sprintf("%s%d", prefix, id)
	return DeleteCachedEntity(h, ctx, key)
}

// CacheWithStringID 使用字符串 ID 缓存
func CacheWithStringID[T any](h *Helper, ctx context.Context, prefix string, id string, entity *T, ttl time.Duration) error {
	key := prefix + id
	return CacheEntity(h, ctx, key, entity, ttl)
}

// GetCachedWithStringID 使用字符串 ID 获取缓存
func GetCachedWithStringID[T any](h *Helper, ctx context.Context, prefix string, id string, dest *T) error {
	key := prefix + id
	return GetCachedEntity(h, ctx, key, dest)
}

// DeleteCachedWithStringID 使用字符串 ID 删除缓存
func DeleteCachedWithStringID(h *Helper, ctx context.Context, prefix string, id string) error {
	key := prefix + id
	return DeleteCachedEntity(h, ctx, key)
}

// EntityCache 实体缓存包装器
type EntityCache[T any] struct {
	helper     *Helper
	prefix     string
	defaultTTL time.Duration
}

// NewEntityCache 创建实体缓存
func NewEntityCache[T any](helper *Helper, prefix string, ttl time.Duration) *EntityCache[T] {
	return &EntityCache[T]{
		helper:     helper,
		prefix:     prefix,
		defaultTTL: ttl,
	}
}

// Set 设置缓存
func (ec *EntityCache[T]) Set(ctx context.Context, id string, entity *T) error {
	return CacheWithStringID(ec.helper, ctx, ec.prefix, id, entity, ec.defaultTTL)
}

// SetWithUintID 使用 uint ID 设置缓存
func (ec *EntityCache[T]) SetWithUintID(ctx context.Context, id uint, entity *T) error {
	return CacheWithID(ec.helper, ctx, ec.prefix, id, entity, ec.defaultTTL)
}

// Get 获取缓存
func (ec *EntityCache[T]) Get(ctx context.Context, id string, dest *T) error {
	return GetCachedWithStringID(ec.helper, ctx, ec.prefix, id, dest)
}

// GetWithUintID 使用 uint ID 获取缓存
func (ec *EntityCache[T]) GetWithUintID(ctx context.Context, id uint, dest *T) error {
	return GetCachedWithID(ec.helper, ctx, ec.prefix, id, dest)
}

// Delete 删除缓存
func (ec *EntityCache[T]) Delete(ctx context.Context, id string) error {
	return DeleteCachedWithStringID(ec.helper, ctx, ec.prefix, id)
}

// DeleteWithUintID 使用 uint ID 删除缓存
func (ec *EntityCache[T]) DeleteWithUintID(ctx context.Context, id uint) error {
	return DeleteCachedWithID(ec.helper, ctx, ec.prefix, id)
}

// BatchDelete 批量删除
func (ec *EntityCache[T]) BatchDelete(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := ec.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// BatchDeleteWithUintID 批量删除 uint ID
func (ec *EntityCache[T]) BatchDeleteWithUintID(ctx context.Context, ids []uint) error {
	for _, id := range ids {
		if err := ec.DeleteWithUintID(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
