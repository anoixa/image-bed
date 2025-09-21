package types

import (
	"errors"
	"time"
)

// Cache 缓存接口
type Cache interface {
	// Set 设置缓存项
	Set(key string, value interface{}, expiration time.Duration) error

	// Get 获取缓存项
	Get(key string, dest interface{}) error

	// Delete 删除缓存项
	Delete(key string) error

	// Exists 检查缓存项是否存在
	Exists(key string) (bool, error)

	// Close 关闭缓存连接
	Close() error
}

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = &cacheMissError{}

type cacheMissError struct{}

func (e *cacheMissError) Error() string {
	return "cache miss"
}

// IsCacheMiss 判断是否为缓存未命中错误
func IsCacheMiss(err error) bool {
	var cacheMissError *cacheMissError
	ok := errors.As(err, &cacheMissError)
	return ok
}
