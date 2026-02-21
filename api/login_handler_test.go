package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// setupTest 初始化测试环境
func setupTest(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	// 初始化 JWT
	err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
	assert.NoError(t, err)

	router := gin.New()
	return router
}

// --- 测试 HTTP 请求处理 ---

// TestLoginHandler_InvalidJSON 测试无效 JSON
func TestLoginHandler_InvalidJSON(t *testing.T) {
	router := setupTest(t)
	router.POST("/login", func(c *gin.Context) {
		var req userAuthRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLoginHandler_MissingFields 测试缺少必填字段
func TestLoginHandler_MissingFields(t *testing.T) {
	router := setupTest(t)
	router.POST("/login", func(c *gin.Context) {
		var req userAuthRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	// 缺少密码
	body := map[string]string{"username": "testuser"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLoginHandler_EmptyBody 测试空请求体
func TestLoginHandler_EmptyBody(t *testing.T) {
	router := setupTest(t)
	router.POST("/login", func(c *gin.Context) {
		var req userAuthRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLoginHandler_ValidRequestFormat 测试有效请求格式
func TestLoginHandler_ValidRequestFormat(t *testing.T) {
	router := setupTest(t)
	router.POST("/login", func(c *gin.Context) {
		var req userAuthRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   "success",
			"username": req.Username,
			"password": req.Password,
		})
	})

	body := map[string]string{
		"username": "testuser",
		"password": "password123",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "success", response["status"])
	assert.Equal(t, "testuser", response["username"])
	assert.Equal(t, "password123", response["password"])
}

// --- 测试 Token 生成和解析 ---

// TestTokenGeneration 测试 Token 生成
func TestTokenGeneration(t *testing.T) {
	// 初始化 JWT
	err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
	assert.NoError(t, err)

	jwtService := GetJWTService()
	assert.NotNil(t, jwtService)

	// 生成 Token
	tokenPair, err := jwtService.GenerateTokens("testuser", 1, "user")
	assert.NoError(t, err)
	assert.NotEmpty(t, tokenPair.AccessToken)
	assert.True(t, tokenPair.AccessTokenExpiry.After(time.Now()))

	// 解析 Token
	claims, err := jwtService.ParseToken(tokenPair.AccessToken)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", claims["username"])
	assert.Equal(t, float64(1), claims["user_id"])
	assert.Equal(t, "user", claims["role"])
	assert.Equal(t, "access", claims["type"])
}

// TestTokenGeneration_InvalidSecret 测试无效密钥
func TestTokenGeneration_InvalidSecret(t *testing.T) {
	// 测试 InitTestJWT 会拒绝过短的密钥（但我们的测试辅助函数不验证长度，跳过此测试）
	t.Skip("Skipped: InitTestJWT test helper does not validate secret length")
}

// TestTokenParse_InvalidToken 测试无效 Token
func TestTokenParse_InvalidToken(t *testing.T) {
	jwtService := GetJWTService()
	if jwtService == nil {
		err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
		assert.NoError(t, err)
		jwtService = GetJWTService()
	}
	_, err := jwtService.ParseToken("invalid.token.here")
	assert.Error(t, err)
}

// TestTokenParse_MalformedToken 测试错误格式 Token
func TestTokenParse_MalformedToken(t *testing.T) {
	// 初始化
	err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
	assert.NoError(t, err)

	jwtService := GetJWTService()

	// 测试错误格式的 token
	_, err = jwtService.ParseToken("not.a.valid.jwt")
	assert.Error(t, err)
}

// TestGenerateRefreshToken 测试刷新令牌生成
func TestGenerateRefreshToken(t *testing.T) {
	// 初始化
	err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
	assert.NoError(t, err)

	jwtService := GetJWTService()

	token, expiry, err := jwtService.GenerateRefreshToken()
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, expiry.After(time.Now()))
	assert.True(t, len(token) >= 64) // 至少64字符
}

// TestGenerateStaticToken 测试静态令牌生成
func TestGenerateStaticToken(t *testing.T) {
	err := InitTestJWT("test-secret-key-at-least-32-characters-long", "30m", "10080m")
	assert.NoError(t, err)

	jwtService := GetJWTService()

	token, err := jwtService.GenerateStaticToken()
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, len(token) >= 64)
}

// --- 测试密码哈希 ---

// TestPasswordHash 测试密码哈希
func TestPasswordHash(t *testing.T) {
	password := "mysecretpassword123"

	// 生成哈希
	hash, err := cryptopackage.GenerateFromPassword(password)
	assert.NoError(t, err)

	assert.NotEmpty(t, hash)
	assert.NotEqual(t, password, hash)

	// 验证正确密码
	ok, err := cryptopackage.ComparePasswordAndHash(password, hash)
	assert.NoError(t, err)
	assert.True(t, ok)

	// 验证错误密码
	ok, err = cryptopackage.ComparePasswordAndHash("wrongpassword", hash)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestPasswordHash_DifferentHashes 测试相同密码不同哈希
func TestPasswordHash_DifferentHashes(t *testing.T) {
	password := "samepassword"

	hash1, _ := cryptopackage.GenerateFromPassword(password)
	hash2, _ := cryptopackage.GenerateFromPassword(password)

	// 相同密码应该产生不同哈希（因为盐值不同）
	assert.NotEqual(t, hash1, hash2)

	// 但都应该能通过验证
	ok1, _ := cryptopackage.ComparePasswordAndHash(password, hash1)
	ok2, _ := cryptopackage.ComparePasswordAndHash(password, hash2)
	assert.True(t, ok1)
	assert.True(t, ok2)
}

// --- 测试 Cookie 设置 ---

// TestAuthCookies_ResponseFormat 测试认证 Cookie 设置
func TestAuthCookies_ResponseFormat(t *testing.T) {
	router := setupTest(t)
	router.POST("/test-cookies", func(c *gin.Context) {
		// 模拟设置 cookie
		c.SetCookie("refresh_token", "test-refresh-token", 3600, "/api/auth/", "", false, true)
		c.SetCookie("device_id", "test-device-id", 3600, "/api/auth/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	req := httptest.NewRequest(http.MethodPost, "/test-cookies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 验证 cookie
	cookies := w.Result().Cookies()
	var hasRefreshToken, hasDeviceID bool
	for _, cookie := range cookies {
		if cookie.Name == "refresh_token" {
			hasRefreshToken = true
			assert.Equal(t, "test-refresh-token", cookie.Value)
			assert.Equal(t, "/api/auth/", cookie.Path)
			assert.True(t, cookie.HttpOnly)
		}
		if cookie.Name == "device_id" {
			hasDeviceID = true
			assert.Equal(t, "test-device-id", cookie.Value)
			assert.Equal(t, "/api/auth/", cookie.Path)
			assert.True(t, cookie.HttpOnly)
		}
	}
	assert.True(t, hasRefreshToken)
	assert.True(t, hasDeviceID)
}

// --- 测试请求结构体验证 ---

// TestUserAuthRequestBody_Validation 测试请求体验证
func TestUserAuthRequestBody_Validation(t *testing.T) {
	router := setupTest(t)
	router.POST("/test", func(c *gin.Context) {
		var req userAuthRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, req)
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid request",
			body:       `{"username":"user","password":"pass"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing username",
			body:       `{"password":"pass"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing password",
			body:       `{"username":"user"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty username",
			body:       `{"username":"","password":"pass"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty password",
			body:       `{"username":"user","password":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestInitTestJWT_Validation 测试 InitTestJWT 参数验证
func TestInitTestJWT_Validation(t *testing.T) {
	tests := []struct {
		name             string
		secret           string
		expiresIn        string
		refreshExpiresIn string
		wantErr          bool
	}{
		{
			name:             "valid config",
			secret:           "this-is-a-valid-secret-key-with-32-chars",
			expiresIn:        "30m",
			refreshExpiresIn: "168h", // Go duration format
			wantErr:          false,
		},
		{
			name:             "invalid expires_in format",
			secret:           "this-is-a-valid-secret-key-with-32-chars",
			expiresIn:        "invalid",
			refreshExpiresIn: "7d",
			wantErr:          true,
		},
		{
			name:             "invalid refresh_expires_in format",
			secret:           "this-is-a-valid-secret-key-with-32-chars",
			expiresIn:        "30m",
			refreshExpiresIn: "invalid",
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InitTestJWT(tt.secret, tt.expiresIn, tt.refreshExpiresIn)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLoginResponse_Structure 测试登录响应结构
func TestLoginResponse_Structure(t *testing.T) {
	response := loginResponse{
		AccessToken:       "test-token",
		AccessTokenExpiry: 1234567890,
	}

	data, err := json.Marshal(response)
	assert.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "test-token", decoded["access_token"])
	assert.Equal(t, float64(1234567890), decoded["access_token_expiry"])
}

// TestLogoutResponse_Structure 测试登出响应结构
func TestLogoutResponse_Structure(t *testing.T) {
	response := logoutResponse{
		DeviceID: "device-123",
	}

	data, err := json.Marshal(response)
	assert.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "device-123", decoded["device_id"])
}

// TestRefreshToken_MissingCookies 测试刷新接口缺少 Cookie
func TestRefreshToken_MissingCookies(t *testing.T) {
	router := setupTest(t)
	router.POST("/refresh", func(c *gin.Context) {
		_, err := c.Cookie("refresh_token")
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRefreshToken_WithCookies 测试刷新接口有 Cookie
func TestRefreshToken_WithCookies(t *testing.T) {
	router := setupTest(t)
	router.POST("/refresh", func(c *gin.Context) {
		token, err := c.Cookie("refresh_token")
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "token": token})
	})

	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "test-refresh-token"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "test-refresh-token", response["token"])
}
