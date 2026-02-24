package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

type ConcurrencyLimiter struct {
	sem *semaphore.Weighted
}

// NewConcurrencyLimiter 并发限制器
func NewConcurrencyLimiter(maxConcurrency int64) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		sem: semaphore.NewWeighted(maxConcurrency),
	}
}

// Middleware 返回 Gin 中间件
func (cl *ConcurrencyLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cl.sem.TryAcquire(1) {
			common.RespondErrorAbort(c, http.StatusServiceUnavailable, "Server is busy, please try again later")
			return
		}

		defer cl.sem.Release(1)

		c.Next()
	}
}

// MiddlewareWithBlock 阻塞但不是立即拒绝
func (cl *ConcurrencyLimiter) MiddlewareWithBlock(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		err := cl.sem.Acquire(ctx, 1)
		if err != nil {
			common.RespondErrorAbort(c, http.StatusServiceUnavailable, "Request timed out waiting for server resources")
			return
		}

		defer cl.sem.Release(1)

		c.Next()
	}
}
