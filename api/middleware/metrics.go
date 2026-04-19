package middleware

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	requestCount    atomic.Int64
	requestDuration atomic.Int64 // in milliseconds
)

// Metrics 基础监控指标中间件
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 响应完成后记录指标
		c.Writer.Header().Set("X-Request-Count", fmt.Sprintf("%d", requestCount.Load()))

		c.Next()

		duration := time.Since(startTime)
		requestDuration.Add(duration.Milliseconds())
		requestCount.Add(1)
	}
}

// GetMetrics 获取当前指标
func GetMetrics() map[string]any {
	count := requestCount.Load()
	duration := requestDuration.Load()
	return map[string]any{
		"request_count":       count,
		"request_duration_ms": duration,
		"avg_duration_ms": func() float64 {
			if count > 0 {
				return float64(duration) / float64(count)
			}
			return 0
		}(),
	}
}
