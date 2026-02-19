package api

import (
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/internal/services/auth"

	"github.com/gin-gonic/gin"
)

// LoginHandler 登录处理器
type LoginHandler struct {
	loginService *auth.LoginService
}

// NewLoginHandlerWithService 使用 LoginService 创建登录处理器
func NewLoginHandlerWithService(loginService *auth.LoginService) *LoginHandler {
	return &LoginHandler{
		loginService: loginService,
	}
}

// SetLoginService 设置登录服务（用于依赖注入）
func (h *LoginHandler) SetLoginService(loginService *auth.LoginService) {
	h.loginService = loginService
}

type userAuthRequestBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	AccessToken       string `json:"access_token"`
	AccessTokenExpiry int64  `json:"access_token_expiry"`
}

type logoutResponse struct {
	DeviceID string `json:"device_id"`
}

// LoginHandlerFunc user login
func (h *LoginHandler) LoginHandlerFunc(context *gin.Context) {
	if h.loginService == nil {
		common.RespondError(context, http.StatusInternalServerError, "Login service not initialized")
		return
	}

	var req userAuthRequestBody
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}

	// 执行登录
	result, err := h.loginService.Login(req.Username, req.Password)
	if err != nil {
		// 检查是否是凭据错误
		if err.Error() == "invalid credentials" {
			common.RespondError(context, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		common.RespondError(context, http.StatusInternalServerError, "Internal server error")
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
func (h *LoginHandler) LogoutHandlerFunc(context *gin.Context) {
	deviceID, err := context.Cookie("device_id")
	if err != nil {
		common.RespondSuccessMessage(context, "Already logged out or session invalid", nil)
		return
	}

	if h.loginService != nil {
		_ = h.loginService.Logout(deviceID)
	}

	clearAuthCookies(context)

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
func clearAuthCookies(c *gin.Context) {
	cfg := config.Get()

	path := "/api/auth/"
	domain := cfg.ServerDomain

	// 将 MaxAge 设置为 -1 来让浏览器删除 Cookie
	c.SetCookie("refresh_token", "", -1, path, domain, false, true)
	c.SetCookie("device_id", "", -1, path, domain, false, true)
}
