package ristretto

import (
	"encoding/json"
	"time"

	"github.com/anoixa/image-bed/cache/types"
	"github.com/dgraph-io/ristretto"
)

// Ristretto 实现缓存接口
type Ristretto struct {
	client *ristretto.Cache
}

// Config Ristretto配置
type Config struct {
	NumCounters int64
	MaxCost     int64
	BufferItems int64
	Metrics     bool
}

// NewRistretto 创建新的Ristretto实例
func NewRistretto(config Config) (types.Cache, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: config.NumCounters,
		MaxCost:     config.MaxCost,
		BufferItems: config.BufferItems,
		Metrics:     config.Metrics,
	})

	if err != nil {
		return nil, err
	}

	return &Ristretto{
		client: cache,
	}, nil
}

// Set 设置缓存项
func (r *Ristretto) Set(key string, value interface{}, expiration time.Duration) error {
	size := int64(1) // 默认大小
	if data, ok := value.([]byte); ok {
		size = int64(len(data))
	}

	set := r.client.SetWithTTL(key, value, size, expiration)
	if set {
		// 等待值被实际设置
		r.client.Wait()
	}
	return nil
}

// Get 获取缓存项
func (r *Ristretto) Get(key string, dest interface{}) error {
	value, found := r.client.Get(key)
	if !found {
		return types.ErrCacheMiss
	}

	switch dest := dest.(type) {
	case *[]byte:
		if data, ok := value.([]byte); ok {
			*dest = data
		} else {
			jsonData, err := json.Marshal(value)
			if err != nil {
				return types.ErrCacheMiss
			}
			*dest = jsonData
		}
	default:
		var data []byte
		if byteData, ok := value.([]byte); ok {
			data = byteData
		} else {
			jsonData, err := json.Marshal(value)
			if err != nil {
				return types.ErrCacheMiss
			}
			data = jsonData
		}

		if err := json.Unmarshal(data, dest); err != nil {
			return types.ErrCacheMiss
		}
	}

	return nil
}

// Delete 删除缓存项
func (r *Ristretto) Delete(key string) error {
	r.client.Del(key)
	return nil
}

// Exists 检查缓存项是否存在
func (r *Ristretto) Exists(key string) (bool, error) {
	_, found := r.client.Get(key)
	return found, nil
}

// Close 关闭缓存连接
func (r *Ristretto) Close() error {
	r.client.Close()
	return nil
}
