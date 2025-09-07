package middleware

import (
	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Authorize 检查context中的认证类型是否在允许的列表中
func Authorize(allowedTypes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authTypeVal, exists := c.Get(AuthTypeKey)
		if !exists {
			common.RespondError(c, http.StatusForbidden, "Access denied. Not authenticated.")
			c.Abort()
			return
		}

		authType, ok := authTypeVal.(string)
		if !ok {
			common.RespondError(c, http.StatusInternalServerError, "Internal error: invalid auth type in context.")
			c.Abort()
			return
		}

		for _, allowed := range allowedTypes {
			if authType == allowed {
				c.Next()
				return
			}
		}

		common.RespondError(c, http.StatusForbidden, "Access denied. You do not have permission to access this resource with this authentication method.")
		c.Abort()
	}
}
