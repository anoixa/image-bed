package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = errors.New("cache miss")

// Redis 实现接口
type Redis struct {
	client *redis.Client
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

// Health 检查 Redis 健康状态
func (r *Redis) Health(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Config 是 redis 包公开的 Redis 配置结构
// 用于从数据库配置创建 Redis 客户端
type Config struct {
	Address            string
	Password           string
	DB                 int
	PoolSize           int
	MinIdleConns       int
	MaxConnAge         string
	PoolTimeout        string
	IdleTimeout        string
	IdleCheckFrequency string
}

// NewRedisFromConfig 使用 Config 创建 Redis 客户端（用于测试配置）
func NewRedisFromConfig(cfg *Config) (*Redis, error) {
	// 解析时间配置
	var maxConnAge, poolTimeout, idleTimeout, idleCheckFrequency time.Duration

	if cfg.MaxConnAge != "" {
		maxConnAge, _ = time.ParseDuration(cfg.MaxConnAge)
	}
	if cfg.PoolTimeout != "" {
		poolTimeout, _ = time.ParseDuration(cfg.PoolTimeout)
	}
	if cfg.IdleTimeout != "" {
		idleTimeout, _ = time.ParseDuration(cfg.IdleTimeout)
	}
	if cfg.IdleCheckFrequency != "" {
		idleCheckFrequency, _ = time.ParseDuration(cfg.IdleCheckFrequency)
	}

	client := redis.NewClient(&redis.Options{
		Addr:               cfg.Address,
		Password:           cfg.Password,
		DB:                 cfg.DB,
		PoolSize:           cfg.PoolSize,
		MinIdleConns:       cfg.MinIdleConns,
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
