package middleware

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// clientLimiter 单个客户端的限流器
type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiterShard 单个分片
type rateLimiterShard struct {
	mu       sync.Mutex
	limiters map[string]*clientLimiter
}

// IPRateLimiter 分片 IP 限流器
type IPRateLimiter struct {
	rps        rate.Limit
	burst      int
	expireTime time.Duration
	shards     []*rateLimiterShard
	numShards  int
	maxSize    int
	stopChan   chan struct{}
}

// NewIPRateLimiter 创建分片 IP 限流器
func NewIPRateLimiter(rps float64, burst int, expireTime time.Duration) *IPRateLimiter {
	numShards := 32
	shards := make([]*rateLimiterShard, numShards)

	for i := 0; i < numShards; i++ {
		shards[i] = &rateLimiterShard{
			limiters: make(map[string]*clientLimiter),
		}
	}

	rl := &IPRateLimiter{
		rps:        rate.Limit(rps),
		burst:      burst,
		expireTime: expireTime,
		shards:     shards,
		numShards:  numShards,
		maxSize:    10000,
		stopChan:   make(chan struct{}),
	}

	// 启动后台清理 goroutine，每 5 分钟扫描一次
	go rl.cleanupStaleClients()

	return rl
}

// Middleware 返回 Gin 中间件
func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := getClientIP(c)
		client := rl.getClientLimiter(ip)

		if !client.limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "Too many requests",
			})
			return
		}

		c.Next()
	}
}

// StopCleanup 停止清理 goroutine
func (rl *IPRateLimiter) StopCleanup() {
	close(rl.stopChan)
}

// getShardIndex 使用 FNV 哈希获取 IP 对应的分片索引
func (rl *IPRateLimiter) getShardIndex(ip string) int {
	h := fnv.New32a()
	h.Write([]byte(ip))
	return int(h.Sum32()) % rl.numShards
}

// getClientLimiter 获取或创建客户端限流器
func (rl *IPRateLimiter) getClientLimiter(ip string) *clientLimiter {
	shardIdx := rl.getShardIndex(ip)
	shard := rl.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 更新 lastSeen 时检查过期
	if client, ok := shard.limiters[ip]; ok {
		if time.Since(client.lastSeen) > rl.expireTime {
			// 已过期，删除旧条目
			delete(shard.limiters, ip)
		} else {
			client.lastSeen = time.Now()
			return client
		}
	}

	// 检查总容量，如果超过则清理过期条目
	if len(shard.limiters) >= rl.maxSize/rl.numShards {
		rl.evictExpiredLocked(shard)
	}

	// 创建新的限流器
	limiter := rate.NewLimiter(rl.rps, rl.burst)
	client := &clientLimiter{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	shard.limiters[ip] = client

	return client
}

// evictExpiredLocked 清理过期条目
func (rl *IPRateLimiter) evictExpiredLocked(shard *rateLimiterShard) {
	now := time.Now()
	for ip, client := range shard.limiters {
		if now.Sub(client.lastSeen) > rl.expireTime {
			delete(shard.limiters, ip)
		}
	}
}

// cleanupStaleClients 后台清理过期客户端
func (rl *IPRateLimiter) cleanupStaleClients() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanupAllShards()
		case <-rl.stopChan:
			return
		}
	}
}

// cleanupAllShards 清理所有分片
func (rl *IPRateLimiter) cleanupAllShards() {
	now := time.Now()
	for _, shard := range rl.shards {
		shard.mu.Lock()
		for ip, client := range shard.limiters {
			if now.Sub(client.lastSeen) > rl.expireTime {
				delete(shard.limiters, ip)
			}
		}
		shard.mu.Unlock()
	}
}

// MaxBytesReader 请求体大小限制中间件（使用 MaxBytesReader 防止内存攻击）
func MaxBytesReader(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}

// RequestSizeLimit 请求体大小限制中间件（已废弃，使用 MaxBytesReader）
// Deprecated: Use MaxBytesReader instead
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return MaxBytesReader(maxSize)
}

// getClientIP 获取客户端真实 IP
func getClientIP(c *gin.Context) string {
	return c.ClientIP()
}

// RequestID 请求 ID 追踪中间件
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}

		c.Set("RequestID", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()
	}
}

func generateRequestID() string {
	now := time.Now()
	return fmt.Sprintf("%d-%d", now.UnixNano(), now.Nanosecond())
}
