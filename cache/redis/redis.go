package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/anoixa/image-bed/cache/types"
	"github.com/go-redis/redis/v8"
)

// Redis 实现了types.Cache接口
type Redis struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedis 创建一个新的Redis实例
func NewRedis(addr, password string, db int) (types.Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 测试连接
	ctx := context.Background()
	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}

	return &Redis{
		client: client,
		ctx:    ctx,
	}, nil
}

// Set 设置缓存项
func (r *Redis) Set(key string, value interface{}, expiration time.Duration) error {
	// 将值序列化为JSON以便存储
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return r.client.Set(r.ctx, key, data, expiration).Err()
}

// Get 获取缓存项
func (r *Redis) Get(key string, dest interface{}) error {
	data, err := r.client.Get(r.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return types.ErrCacheMiss
		}
		return err
	}

	// 将数据反序列化到目标结构
	if err := json.Unmarshal([]byte(data), dest); err != nil {
		return err
	}

	return nil
}

// Delete 删除缓存项
func (r *Redis) Delete(key string) error {
	return r.client.Del(r.ctx, key).Err()
}

// Exists 检查缓存项是否存在
func (r *Redis) Exists(key string) (bool, error) {
	exists, err := r.client.Exists(r.ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// Close 关闭缓存连接
func (r *Redis) Close() error {
	return r.client.Close()
}
