package middleware

import (
	"net/http"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
)

const (
	ContextUserIDKey   = "user_id"
	ContextUsernameKey = "username"
)

// Auth Authentication middleware
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取并验证 Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			common.RespondError(c, http.StatusUnauthorized, "Authorization header is required")
			c.Abort()
			return
		}
		claims, err := api.Parse(authHeader)
		if err != nil {
			common.RespondError(c, http.StatusUnauthorized, "Invalid or expired token")
			c.Abort()
			return
		}

		userID, ok := claims["user_id"].(float64)
		if !ok {
			common.RespondError(c, http.StatusUnauthorized, "Invalid user ID in token")
			c.Abort()
			return
		}

		username, ok := claims["username"].(string)
		if !ok {
			common.RespondError(c, http.StatusUnauthorized, "Invalid username in token")
			c.Abort()
			return
		}

		// 将用户信息存入上下文
		c.Set(ContextUserIDKey, uint(userID))
		c.Set(ContextUsernameKey, username)

		c.Next()
	}
}
