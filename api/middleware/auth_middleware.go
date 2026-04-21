package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/internal/auth"
	"github.com/gin-gonic/gin"
)

func CombinedAuth(jwtService *auth.JWTService) gin.HandlerFunc {
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
		case AuthSchemeBearer:
			err = handleJwtAuth(c, token, jwtService)
		case AuthSchemeAPIKey:
			err = handleStaticTokenAuth(c, token, jwtService)
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

func handleJwtAuth(c *gin.Context, token string, jwtService *auth.JWTService) error {
	if jwtService == nil {
		return errors.New("JWT service not initialized")
	}
	claims, err := jwtService.ParseToken(token)
	if err != nil {
		return errors.New("invalid or expired token")
	}

	if claims.UserID == 0 {
		return errors.New("user_id not found in token claims")
	}

	if claims.Username == "" {
		return errors.New("username not found in token claims")
	}

	role := claims.Role
	if role == "" {
		role = RoleUser
	}

	c.Set(ContextUserIDKey, claims.UserID)
	c.Set(ContextUsernameKey, claims.Username)
	c.Set(ContextRoleKey, role)
	c.Set(AuthTypeKey, AuthTypeJWT)

	return nil
}

func handleStaticTokenAuth(c *gin.Context, token string, jwtService *auth.JWTService) error {
	if jwtService == nil {
		return errors.New("JWT service not initialized")
	}
	user, err := jwtService.ValidateStaticToken(c.Request.Context(), token)
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
