package memory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dgraph-io/ristretto"
)

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = errors.New("cache miss")

// Memory 内存缓存实现
type Memory struct {
	client *ristretto.Cache
}

// Config 内存缓存配置
type Config struct {
	NumCounters int64
	MaxCost     int64
	BufferItems int64
	Metrics     bool
}

// NewMemory 创建新的内存缓存提供者
func NewMemory(config Config) (*Memory, error) {
	client, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: config.NumCounters,
		MaxCost:     config.MaxCost,
		BufferItems: config.BufferItems,
		Metrics:     config.Metrics,
	})

	if err != nil {
		return nil, err
	}

	return &Memory{
		client: client,
	}, nil
}

// Set 设置缓存项
func (m *Memory) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	size := int64(1) // 默认大小
	if data, ok := value.([]byte); ok {
		size = int64(len(data))
	}

	set := m.client.SetWithTTL(key, value, size, expiration)
	if set {
		// 等待值被实际设置
		m.client.Wait()
	}
	return nil
}

// Get 获取缓存项
func (m *Memory) Get(ctx context.Context, key string, dest interface{}) error {
	value, found := m.client.Get(key)
	if !found {
		return ErrCacheMiss
	}

	switch dest := dest.(type) {
	case *[]byte:
		if data, ok := value.([]byte); ok {
			*dest = data
		} else {
			jsonData, err := json.Marshal(value)
			if err != nil {
				return ErrCacheMiss
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
				return ErrCacheMiss
			}
			data = jsonData
		}

		if err := json.Unmarshal(data, dest); err != nil {
			return ErrCacheMiss
		}
	}

	return nil
}

// Delete 删除缓存项
func (m *Memory) Delete(ctx context.Context, key string) error {
	m.client.Del(key)
	return nil
}

// Exists 检查缓存项是否存在
func (m *Memory) Exists(ctx context.Context, key string) (bool, error) {
	_, found := m.client.Get(key)
	return found, nil
}

// Close 关闭缓存连接
func (m *Memory) Close() error {
	m.client.Close()
	return nil
}

// Name 返回缓存提供者名称
func (m *Memory) Name() string {
	return "memory"
}
