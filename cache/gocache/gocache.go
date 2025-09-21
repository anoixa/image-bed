package gocache

import (
	"encoding/json"
	"time"

	"github.com/anoixa/image-bed/cache/types"
	gocachepkg "github.com/patrickmn/go-cache"
)

// GoCache 实现接口
type GoCache struct {
	client *gocachepkg.Cache
}

// NewGoCache 创建新的GoCache实例
func NewGoCache(defaultExpiration, cleanupInterval time.Duration) types.Cache {
	gc := &GoCache{
		client: gocachepkg.New(defaultExpiration, cleanupInterval),
	}
	return gc
}

// Set 设置缓存项
func (g *GoCache) Set(key string, value interface{}, expiration time.Duration) error {
	// 序列化json
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	g.client.Set(key, data, expiration)
	return nil
}

// Get 获取缓存项
func (g *GoCache) Get(key string, dest interface{}) error {
	data, found := g.client.Get(key)
	if !found {
		return types.ErrCacheMiss
	}

	// 反序列化
	if err := json.Unmarshal(data.([]byte), dest); err != nil {
		return err
	}

	return nil
}

// Delete 删除缓存项
func (g *GoCache) Delete(key string) error {
	g.client.Delete(key)
	return nil
}

// Exists 检查缓存项是否存在
func (g *GoCache) Exists(key string) (bool, error) {
	_, found := g.client.Get(key)
	return found, nil
}

// Close 关闭缓存连接
func (g *GoCache) Close() error {
	// GoCache不需要显式关闭连接
	return nil
}
