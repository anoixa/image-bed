package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
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

		newLimiter := rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
		val, _ := rl.limiterMap.LoadOrStore(ip, &clientLimiter{
			limiter:  newLimiter,
			lastSeen: time.Now(),
		})

		client := val.(*clientLimiter)
		client.lastSeen = time.Now()

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
				if time.Since(client.lastSeen) > rl.expireTime {
					rl.limiterMap.Delete(key)
				}
				return true
			})
		case <-rl.stopChan:
			return
		}
	}
}

// getClientIP Get the client's real IP address
func getClientIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return ip
	}
	return c.ClientIP()
}
