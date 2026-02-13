package cache

import (
	"context"
	"time"
)

// Provider 缓存提供者接口 - 依赖倒置的核心抽象
type Provider interface {
	// Set 设置缓存项
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error

	// Get 获取缓存项
	Get(ctx context.Context, key string, dest interface{}) error

	// Delete 删除缓存项
	Delete(ctx context.Context, key string) error

	// Exists 检查缓存项是否存在
	Exists(ctx context.Context, key string) (bool, error)

	// Close 关闭缓存连接
	Close() error

	// Name 返回缓存提供者名称
	Name() string
}

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = &cacheMissError{}

type cacheMissError struct{}

func (e *cacheMissError) Error() string {
	return "cache miss"
}

// IsCacheMiss 判断是否为缓存未命中错误
func IsCacheMiss(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*cacheMissError)
	return ok
}
