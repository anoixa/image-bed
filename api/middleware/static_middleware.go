package middleware

import (
	"net/http"
	"strings"

	"github.com/anoixa/image-bed/database/key"

	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
)

func StaticTokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}

		if token == "" {
			common.RespondError(c, http.StatusUnauthorized, "API token is required")
			c.Abort()
			return
		}

		user, err := key.GetUserByApiToken(token)
		if err != nil {
			common.RespondError(c, http.StatusUnauthorized, "Invalid API token")
			c.Abort()
			return
		}

		c.Set(ContextUserIDKey, user.ID)
		c.Next()
	}
}
