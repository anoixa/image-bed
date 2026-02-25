package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// SimpleRateLimiter 简化的限流器，使用标准库实现
type SimpleRateLimiter struct {
	limiter  *rate.Limiter
	burst    int
	interval time.Duration
}

// NewSimpleRateLimiter 创建简单限流器
// rps: 每秒请求数
// burst: 突发请求数
func NewSimpleRateLimiter(rps float64, burst int) *SimpleRateLimiter {
	return &SimpleRateLimiter{
		limiter:  rate.NewLimiter(rate.Limit(rps), burst),
		burst:    burst,
		interval: time.Second / time.Duration(rps),
	}
}

// Middleware 返回 Gin 中间件
func (rl *SimpleRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.limiter.Allow() {
			common.RespondErrorAbort(c, http.StatusTooManyRequests, "Too many requests")
			return
		}
		c.Next()
	}
}

// PerClientRateLimiter 每个客户端独立的限流器
type PerClientRateLimiter struct {
	rps             rate.Limit
	burst           int
	clients         map[string]*rate.Limiter
	mu              sync.RWMutex
	cleanupInterval time.Duration
}

// NewPerClientRateLimiter 创建每客户端限流器
func NewPerClientRateLimiter(rps float64, burst int) *PerClientRateLimiter {
	rl := &PerClientRateLimiter{
		rps:             rate.Limit(rps),
		burst:           burst,
		clients:         make(map[string]*rate.Limiter),
		cleanupInterval: 5 * time.Minute,
	}
	// 可选：启动清理协程
	// go rl.cleanup()
	return rl
}

// Middleware 返回 Gin 中间件
func (rl *PerClientRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := getClientIP(c)
		limiter := rl.getLimiter(ip)

		if !limiter.Allow() {
			common.RespondErrorAbort(c, http.StatusTooManyRequests, "Too many requests")
			return
		}
		c.Next()
	}
}

func (rl *PerClientRateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.clients[ip]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 双重检查
	if limiter, exists := rl.clients[ip]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rps, rl.burst)
	rl.clients[ip] = limiter
	return limiter
}
