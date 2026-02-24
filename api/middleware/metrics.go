package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	requestCount    int64
	requestDuration int64 // in milliseconds
)

// Metrics 基础监控指标中间件
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 响应完成后记录指标
		c.Writer.Header().Set("X-Request-Count", fmt.Sprintf("%d", requestCount))

		c.Next()

		duration := time.Since(startTime)
		requestDuration += duration.Milliseconds()
		requestCount++
	}
}

// GetMetrics 获取当前指标
func GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"request_count":       requestCount,
		"request_duration_ms": requestDuration,
		"avg_duration_ms": func() float64 {
			if requestCount > 0 {
				return float64(requestDuration) / float64(requestCount)
			}
			return 0
		}(),
	}
}
