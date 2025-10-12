package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
)

const (
	ContextUserIDKey   = "user_id"
	ContextUsernameKey = "username"
	AuthTypeKey        = "auth_type"

	AuthTypeJWT         = "jwt"
	AuthTypeStaticToken = "static_token"
)

func CombinedAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取 Authorization 头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			common.RespondError(c, http.StatusUnauthorized, "No Authorization request header")
			c.Abort()
			return
		}

		// 解析 Scheme 和 Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			common.RespondError(c, http.StatusBadRequest, "Authorization field format error")
			c.Abort()
			return
		}

		scheme := parts[0]
		token := parts[1]
		var err error

		switch scheme {
		case "Bearer":
			err = handleJwtAuth(c, token)
		case "ApiKey":
			err = handleStaticTokenAuth(c, token)
		default:
			common.RespondError(c, http.StatusUnauthorized, "Unsupported authentication scheme")
			c.Abort()
			return
		}

		if err != nil {
			common.RespondError(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}

		c.Next()
	}
}

func handleJwtAuth(c *gin.Context, token string) error {
	claims, err := api.Parse(token)
	if err != nil {
		return errors.New("invalid or expired token")
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return errors.New("user ID in token is invalid")
	}

	username, ok := claims["username"].(string)
	if !ok {
		return errors.New("invalid user ID in token")
	}

	// 验证成功，将用户信息存入上下文
	c.Set(ContextUserIDKey, uint(userID))
	c.Set(ContextUsernameKey, username)
	c.Set(AuthTypeKey, AuthTypeJWT)

	return nil
}

func handleStaticTokenAuth(c *gin.Context, token string) error {
	user, err := api.ValidateStaticToken(token)
	if err != nil {
		return errors.New("invalid token")
	}

	//将用户信息存入上下文
	c.Set(ContextUserIDKey, user.ID)
	c.Set(ContextUsernameKey, user.Username)
	c.Set(AuthTypeKey, AuthTypeStaticToken)

	return nil
}
