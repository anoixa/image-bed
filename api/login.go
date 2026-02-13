package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	accounts2 "github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/utils"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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

// LoginHandler user login
func LoginHandler(context *gin.Context) {
	var req userAuthRequestBody
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}

	// 验证用户凭据
	user, valid, err := validateCredentials(req.Username, req.Password)
	if err != nil {
		log.Printf("LoginHandler error for user %s: %v\n", utils.SanitizeLogUsername(req.Username), err)
		common.RespondError(context, http.StatusInternalServerError, "Internal server error")
		return
	}
	if !valid {
		common.RespondError(context, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// 生成 JWT access tokens
	accessToken, accessTokenExpiry, err := GenerateTokens(user.Username, user.ID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to generate authentication tokens")
		return
	}

	//生成Refresh Token
	refreshToken, refreshTokenExpiry, err := GenerateRefreshToken()
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to generate refresh tokens")
		return
	}
	// 存储设备信息
	deviceID := uuid.New().String()
	err = accounts2.CreateLoginDevice(user.ID, deviceID, refreshToken, refreshTokenExpiry)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to store device token")
		return
	}

	// 设置 HttpOnly Cookie
	refreshTokenMaxAge := int(time.Until(refreshTokenExpiry).Seconds())
	setAuthCookies(context, refreshToken, deviceID, refreshTokenMaxAge)

	common.RespondSuccessMessage(context, "Login successful", loginResponse{
		AccessToken:       "Bearer " + accessToken,
		AccessTokenExpiry: accessTokenExpiry.Unix(),
	})
}

// RefreshTokenHandler Refresh token authentication
func RefreshTokenHandler(context *gin.Context) {
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

	// 查询设备信息与用户信息
	device, err := accounts2.GetDeviceByRefreshTokenAndDeviceID(refreshToken, deviceID)
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "User associated with token not found")
		return
	}
	user, err := accounts2.GetUserByUserIDWithCache(device.UserID)
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	// 存储设备信息
	newRefreshToken, newRefreshTokenExpiry, err := GenerateRefreshToken()
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to generate refresh tokens")
		return
	}
	err = accounts2.RotateRefreshToken(user.ID, device.DeviceID, newRefreshToken, newRefreshTokenExpiry)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to update device token")
		return
	}

	accessToken, accessTokenExpiry, err := GenerateTokens(user.Username, user.ID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to generate new access tokens")
		return
	}

	// 更新 cookies
	newRefreshTokenMaxAge := int(time.Until(newRefreshTokenExpiry).Seconds())
	setAuthCookies(context, newRefreshToken, deviceID, newRefreshTokenMaxAge)

	common.RespondSuccessMessage(context, "Refresh token successful", loginResponse{
		AccessToken:       "Bearer " + accessToken,
		AccessTokenExpiry: accessTokenExpiry.Unix(),
	})

}

// LogoutHandler user logout
func LogoutHandler(context *gin.Context) {
	deviceID, err := context.Cookie("device_id")
	if err != nil {
		common.RespondSuccessMessage(context, "Already logged out or session invalid", nil)
		return
	}

	_ = accounts2.DeleteDeviceByDeviceID(deviceID)

	clearAuthCookies(context)

	common.RespondSuccessMessage(context, "Logout successful", nil)
}

// validateCredentials Verify user credentials
func validateCredentials(username, password string) (*models.User, bool, error) {
	user, err := accounts2.GetUserByUsername(username)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		return nil, false, nil
	}

	ok, err := cryptopackage.ComparePasswordAndHash(password, user.Password)
	if err != nil {
		return nil, false, fmt.Errorf("password comparison failed: %w", err)
	}

	return user, ok, nil
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
	domain := cfg.Server.Domain

	// 将 MaxAge 设置为 -1 来让浏览器删除 Cookie
	c.SetCookie("refresh_token", "", -1, path, domain, false, true)
	c.SetCookie("device_id", "", -1, path, domain, false, true)
}
