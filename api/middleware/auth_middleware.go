package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
)

// jwtServiceAccessor 获取 JWT 服务
var jwtServiceAccessor = api.GetJWTService

const (
	ContextUserIDKey   = "user_id"
	ContextUsernameKey = "username"
	ContextRoleKey     = "role"
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
	jwtService := jwtServiceAccessor()
	if jwtService == nil {
		return errors.New("JWT service not initialized")
	}
	claims, err := jwtService.ParseToken(token)
	if err != nil {
		return errors.New("invalid or expired token")
	}

	userIDValue, ok := claims["user_id"]
	if !ok {
		return errors.New("user_id not found in token claims")
	}
	userID, ok := userIDValue.(float64)
	if !ok {
		return errors.New("user_id in token is not a valid number")
	}

	usernameValue, ok := claims["username"]
	if !ok {
		return errors.New("username not found in token claims")
	}
	username, ok := usernameValue.(string)
	if !ok {
		return errors.New("username in token is not a valid string")
	}

	role, _ := claims["role"].(string)
	if role == "" {
		role = "user"
	}

	c.Set(ContextUserIDKey, uint(userID))
	c.Set(ContextUsernameKey, username)
	c.Set(ContextRoleKey, role)
	c.Set(AuthTypeKey, AuthTypeJWT)

	return nil
}

func handleStaticTokenAuth(c *gin.Context, token string) error {
	jwtService := jwtServiceAccessor()
	if jwtService == nil {
		return errors.New("JWT service not initialized")
	}
	user, err := jwtService.ValidateStaticToken(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	role := user.Role
	if role == "" {
		role = "user"
	}

	//将用户信息存入上下文
	c.Set(ContextUserIDKey, user.ID)
	c.Set(ContextUsernameKey, user.Username)
	c.Set(ContextRoleKey, role)
	c.Set(AuthTypeKey, AuthTypeStaticToken)

	return nil
}
