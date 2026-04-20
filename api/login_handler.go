package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/internal/auth"
	"github.com/anoixa/image-bed/utils"

	"github.com/gin-gonic/gin"
)

// LoginRequest 登录请求
// swagger:model
// @Description Login request body
type LoginRequest struct {
	// Username
	// required: true
	// example: admin
	Username string `json:"username" binding:"required"`
	// Password
	// required: true
	// example: password123
	Password string `json:"password" binding:"required,max=1024"`
}

// LoginResponse 登录响应
// swagger:model
// @Description Login response
type LoginResponse struct {
	// Access token for API calls
	// example: Bearer eyJhbGciOiJIUzI1NiIs...
	AccessToken string `json:"access_token"`
	// Access token expiry timestamp (Unix)
	// example: 1704067200
	AccessTokenExpiry int64 `json:"access_token_expiry"`
}

// LogoutResponse 登出响应
// swagger:model
// @Description Logout response
type LogoutResponse struct {
	// Device ID that was logged out
	// example: 550e8400-e29b-41d4-a716-446655440000
	DeviceID string `json:"device_id"`
}

// ErrorResponse 错误响应
// swagger:model
// @Description Error response
type ErrorResponse struct {
	// Error message
	// example: Invalid credentials
	Message string `json:"message"`
}

// LoginHandler 登录处理器
type LoginHandler struct {
	loginService *auth.LoginService
	cfg          *config.Config
}

// NewLoginHandlerWithService 使用 LoginService 创建登录处理器
func NewLoginHandlerWithService(loginService *auth.LoginService, cfg *config.Config) *LoginHandler {
	return &LoginHandler{
		loginService: loginService,
		cfg:          cfg,
	}
}

// SetLoginService 设置登录服务
func (h *LoginHandler) SetLoginService(loginService *auth.LoginService) {
	h.loginService = loginService
}

type userAuthRequestBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,max=1024"`
}

type loginResponse struct {
	AccessToken       string `json:"access_token"`
	AccessTokenExpiry int64  `json:"access_token_expiry"`
}

type logoutResponse struct {
	DeviceID string `json:"device_id"`
}

// LoginHandlerFunc user login
// @Summary      User login
// @Description  Authenticate user with username and password, returns access token and sets refresh token cookie
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      userAuthRequestBody  true  "Login credentials"
// @Success      200      {object}  common.Response{data=loginResponse}  "Login successful"
// @Failure      400      {object}  common.Response  "Invalid request body"
// @Failure      401      {object}  common.Response  "Invalid credentials"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Router       /api/auth/login [post]
func (h *LoginHandler) LoginHandlerFunc(context *gin.Context) {
	if h.loginService == nil {
		common.RespondError(context, http.StatusInternalServerError, "Login service not initialized")
		return
	}

	var req userAuthRequestBody
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid request body")
		return
	}

	result, err := h.loginService.Login(req.Username, req.Password)
	if err != nil {
		if strings.Contains(err.Error(), "invalid credentials") {
			common.RespondError(context, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		common.RespondError(context, http.StatusInternalServerError, "Login failed")
		return
	}

	// 设置 HttpOnly Cookie
	refreshTokenMaxAge := int(time.Until(result.RefreshTokenExpiry).Seconds())
	setAuthCookies(context, result.RefreshToken, result.DeviceID, refreshTokenMaxAge)

	common.RespondSuccessMessage(context, "Login successful", loginResponse{
		AccessToken:       "Bearer " + result.AccessToken,
		AccessTokenExpiry: result.AccessTokenExpiry.Unix(),
	})
}

// RefreshTokenHandlerFunc Refresh token authentication
// @Summary      Refresh access token
// @Description  Refresh access token using refresh_token and device_id cookies
// @Tags         auth
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response{data=loginResponse}  "Refresh token successful"
// @Failure      401  {object}  common.Response  "Refresh token or device ID not found / invalid"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Router       /api/auth/refresh [post]
func (h *LoginHandler) RefreshTokenHandlerFunc(context *gin.Context) {
	if h.loginService == nil {
		common.RespondError(context, http.StatusInternalServerError, "Login service not initialized")
		return
	}

	refreshToken, err := context.Cookie("refresh_token")
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "Refresh token not found")
		return
	}

	deviceID, err := context.Cookie("device_id")
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "Device ID not found")
		return
	}

	// 刷新令牌
	result, err := h.loginService.RefreshToken(refreshToken, deviceID)
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	// 更新 cookies
	newRefreshTokenMaxAge := int(time.Until(result.RefreshTokenExpiry).Seconds())
	setAuthCookies(context, result.RefreshToken, deviceID, newRefreshTokenMaxAge)

	common.RespondSuccessMessage(context, "Refresh token successful", loginResponse{
		AccessToken:       "Bearer " + result.AccessToken,
		AccessTokenExpiry: result.AccessTokenExpiry.Unix(),
	})
}

// LogoutHandlerFunc user logout
// @Summary      User logout
// @Description  Logout user by invalidating session. Works with any combination of cookies (refresh_token, device_id, or both). Always clears cookies and returns 200 for idempotency.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response  "Logout successful or already logged out"
// @Router       /api/auth/logout [post]
func (h *LoginHandler) LogoutHandlerFunc(context *gin.Context) {
	deviceID, _ := context.Cookie("device_id")
	refreshToken, _ := context.Cookie("refresh_token")

	// 始终清理客户端 Cookie
	h.clearAuthCookies(context)

	// 两者都缺失：已登出状态
	if deviceID == "" && refreshToken == "" {
		common.RespondSuccessMessage(context, "Already logged out", nil)
		return
	}

	// 至少有一个凭证：执行服务端清理
	if h.loginService != nil {
		// 忽略错误，确保幂等性
		_ = h.loginService.Logout(deviceID, refreshToken)
	}

	common.RespondSuccessMessage(context, "Logout successful", nil)
}

// setAuthCookies 设置 refresh_token 和 device_id 的 cookie
func setAuthCookies(c *gin.Context, refreshToken, deviceID string, maxAge int) {
	path := "/api/auth/"
	secure := config.IsProduction()

	// 构造 refresh_token cookie
	refreshTokenCookie := http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		MaxAge:   maxAge,
		Path:     path,
		Domain:   "",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	// 构造 device_id cookie
	deviceIDCookie := http.Cookie{
		Name:     "device_id",
		Value:    deviceID,
		MaxAge:   maxAge,
		Path:     path,
		Domain:   "",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	http.SetCookie(c.Writer, &refreshTokenCookie)
	http.SetCookie(c.Writer, &deviceIDCookie)
}

// clearAuthCookies 清除认证相关的 cookie
func (h *LoginHandler) clearAuthCookies(c *gin.Context) {
	path := "/api/auth/"
	secure := config.IsProduction()

	// 始终清理 host-only Cookie；登录时默认就是这种形式。
	c.SetCookie("refresh_token", "", -1, path, "", secure, true)
	c.SetCookie("device_id", "", -1, path, "", secure, true)

	// 如果配置了显式域名，再额外清理一次 domain Cookie，兼容历史配置。
	domain := ""
	if h.cfg != nil {
		domain = utils.ExtractCookieDomain(h.cfg.ServerDomain)
	}
	if domain != "" {
		c.SetCookie("refresh_token", "", -1, path, domain, secure, true)
		c.SetCookie("device_id", "", -1, path, domain, secure, true)
	}
}
