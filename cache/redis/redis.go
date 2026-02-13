package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/go-redis/redis/v8"
)

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = errors.New("cache miss")

// Redis 实现接口
type Redis struct {
	client *redis.Client
}

// NewRedis 创建一个新的 Redis 缓存提供者
func NewRedis(cfg *config.Config) (*Redis, error) {
	redisCfg := cfg.Server.CacheConfig.Redis

	// 解析时间配置
	var maxConnAge, poolTimeout, idleTimeout, idleCheckFrequency time.Duration

	if redisCfg.MaxConnAge != "" {
		maxConnAge, _ = time.ParseDuration(redisCfg.MaxConnAge)
	}
	if redisCfg.PoolTimeout != "" {
		poolTimeout, _ = time.ParseDuration(redisCfg.PoolTimeout)
	}
	if redisCfg.IdleTimeout != "" {
		idleTimeout, _ = time.ParseDuration(redisCfg.IdleTimeout)
	}
	if redisCfg.IdleCheckFrequency != "" {
		idleCheckFrequency, _ = time.ParseDuration(redisCfg.IdleCheckFrequency)
	}

	client := redis.NewClient(&redis.Options{
		Addr:               redisCfg.Address,
		Password:           redisCfg.Password,
		DB:                 redisCfg.DB,
		PoolSize:           redisCfg.PoolSize,
		MinIdleConns:       redisCfg.MinIdleConns,
		MaxConnAge:         maxConnAge,
		PoolTimeout:        poolTimeout,
		IdleTimeout:        idleTimeout,
		IdleCheckFrequency: idleCheckFrequency,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, err
	}

	return &Redis{
		client: client,
	}, nil
}

// Set 设置缓存项
func (r *Redis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	var data []byte
	var err error

	if byteData, ok := value.([]byte); ok {
		data = byteData
	} else {
		data, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}

	return r.client.Set(ctx, key, data, expiration).Err()
}

// Get 获取缓存项
func (r *Redis) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrCacheMiss
		}
		return err
	}

	if byteDest, ok := dest.(*[]byte); ok {
		*byteDest = []byte(data)
		return nil
	}

	return json.Unmarshal([]byte(data), dest)
}

// Delete 删除缓存项
func (r *Redis) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Exists 检查缓存项是否存在
func (r *Redis) Exists(ctx context.Context, key string) (bool, error) {
	result, err := r.client.Exists(ctx, key).Result()
	return result > 0, err
}

// Close 关闭缓存连接
func (r *Redis) Close() error {
	return r.client.Close()
}

// Name 返回缓存名称
func (r *Redis) Name() string {
	return "redis"
}
