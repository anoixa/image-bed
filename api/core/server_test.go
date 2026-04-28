package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "OK")
}

func TestConfigureClientIPTrustIgnoresForwardedHeadersWithoutTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	configureClientIPTrust(router, &config.Config{})
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.22.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "172.22.0.1", w.Body.String())
}

func TestConfigureClientIPTrustUsesForwardedHeadersFromTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	configureClientIPTrust(router, &config.Config{
		TrustedProxies: "172.22.0.0/16",
	})
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.22.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "203.0.113.10", w.Body.String())
}

func TestConfigureClientIPTrustSupportsConfiguredRealIPHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	configureClientIPTrust(router, &config.Config{
		TrustedProxies: "172.22.0.0/16",
		RealIPHeaders:  "CF-Connecting-IP",
	})
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.22.0.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "203.0.113.10", w.Body.String())
}
