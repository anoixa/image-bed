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
		if err := authenticateRequest(c, jwtService, false); err != nil {
			common.RespondError(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}

		c.Next()
	}
}

func OptionalCombinedAuth(jwtService *auth.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := authenticateRequest(c, jwtService, true); err != nil {
			common.RespondError(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}

		c.Next()
	}
}

func authenticateRequest(c *gin.Context, jwtService *auth.JWTService, allowMissing bool) error {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		if allowMissing {
			return nil
		}
		return errors.New("no Authorization request header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return errors.New("authorization field format error")
	}

	scheme := parts[0]
	token := parts[1]

	switch scheme {
	case AuthSchemeBearer:
		return handleJwtAuth(c, token, jwtService)
	case AuthSchemeAPIKey:
		return handleStaticTokenAuth(c, token, jwtService)
	default:
		return errors.New("unsupported authentication scheme")
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
