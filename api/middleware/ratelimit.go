package middleware

import (
	"net/http"
	"sync"

	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	mu       sync.Mutex
}

type IPRateLimiter struct {
	rps        float64       // 每秒请求数
	burst      int           // 令牌桶的容量
	expireTime time.Duration // 过期时间
	limiterMap *sync.Map
	stopChan   chan struct{}
}

// NewIPRateLimiter Create new IP-based rate limits
func NewIPRateLimiter(rps float64, burst int, expireTime time.Duration) *IPRateLimiter {
	limiter := &IPRateLimiter{
		rps:        rps,
		burst:      burst,
		expireTime: expireTime,
		limiterMap: &sync.Map{},
		stopChan:   make(chan struct{}),
	}

	// 启动后台清理 goroutine
	go limiter.cleanupStaleClients()

	return limiter
}

// Middleware Return a Gin middleware handler
func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := getClientIP(c)

		var client *clientLimiter
		val, ok := rl.limiterMap.Load(ip)
		if ok {
			client = val.(*clientLimiter)
		} else {
			newLimiter := rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
			client = &clientLimiter{limiter: newLimiter}
			val, _ = rl.limiterMap.LoadOrStore(ip, client)
			client = val.(*clientLimiter)
		}

		client.mu.Lock()
		client.lastSeen = time.Now()
		client.mu.Unlock()

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

func (rl *IPRateLimiter) StopCleanup() {
	close(rl.stopChan)
}

func (rl *IPRateLimiter) cleanupStaleClients() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 遍历 sync.Map，删除过期的条目
			rl.limiterMap.Range(func(key, value interface{}) bool {
				client := value.(*clientLimiter)

				client.mu.Lock()
				lastSeen := client.lastSeen
				client.mu.Unlock()

				if time.Since(lastSeen) > rl.expireTime {
					rl.limiterMap.Delete(key)
				}
				return true
			})
		case <-rl.stopChan:
			return
		}
	}
}

// RequestSizeLimit 请求体大小限制中间件
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxSize {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":       "Request body too large",
				"max_size":    fmt.Sprintf("%d bytes", maxSize),
				"actual_size": fmt.Sprintf("%d bytes", c.Request.ContentLength),
			})
			return
		}
		c.Next()
	}
}

// getClientIP Get the client's real IP address
// Security: Only uses c.ClientIP() to prevent IP forgery attacks
// Do not use X-Forwarded-For or X-Real-IP headers as they can be spoofed
func getClientIP(c *gin.Context) string {
	return c.ClientIP()
}

// RequestID 请求ID追踪中间件
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			// 使用 UUID v4 生成新的请求ID
			requestID = fmt.Sprintf("%s", generateRequestID())
		}

		c.Set("RequestID", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		// 记录请求ID到日志
		// log.Printf("[RequestID: %s] %s %s", requestID, c.Request.Method, c.Request.URL.Path)

		c.Next()
	}
}

func generateRequestID() string {
	now := time.Now()
	return fmt.Sprintf("%d-%d", now.UnixNano(), now.Nanosecond())
}
