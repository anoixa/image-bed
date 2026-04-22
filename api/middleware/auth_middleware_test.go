package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionalCombinedAuthAllowsMissingAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(OptionalCombinedAuth(nil))
	router.GET("/images/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/images/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOptionalCombinedAuthSetsContextForValidBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	jwtService, err := api.NewTestJWTService("0123456789abcdef0123456789abcdef", "15m", "24h")
	require.NoError(t, err)

	tokenPair, err := jwtService.GenerateTokens("owner", 42, RoleUser)
	require.NoError(t, err)

	router := gin.New()
	router.Use(OptionalCombinedAuth(jwtService))
	router.GET("/images/test", func(c *gin.Context) {
		assert.Equal(t, uint(42), c.GetUint(ContextUserIDKey))
		assert.Equal(t, "owner", c.GetString(ContextUsernameKey))
		assert.Equal(t, RoleUser, c.GetString(ContextRoleKey))
		assert.Equal(t, AuthTypeJWT, c.GetString(AuthTypeKey))
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/images/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOptionalCombinedAuthRejectsInvalidAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(OptionalCombinedAuth(nil))
	router.GET("/images/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/images/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
