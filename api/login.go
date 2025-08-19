package api

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"image-bed/api/common"
	"image-bed/database/accounts"
	"image-bed/database/models"
	cryptopackage "image-bed/utils/crypto"
	"log"
	"net/http"
)

type userAuthRequestBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	AccessToken        string `json:"access_token"`
	AccessTokenExpiry  int64  `json:"access_token_expiry"`
	RefreshToken       string `json:"refresh_token"`
	RefreshTokenExpiry int64  `json:"refresh_token_expiry"`
	DeviceID           string `json:"device_id"`
}

type logoutResponse struct {
	DeviceID string `json:"device_id"`
}

type refreshRuestBody struct {
	GrantType    string `json:"grant_type" binding:"required"`
	RefreshToken string `json:"refresh_token" binding:"required"`
	DeviceID     string `json:"device_id" binding:"required"`
}

// LoginHandler user login
func LoginHandler(context *gin.Context) {
	var req userAuthRequestBody
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
	}

	// 验证用户凭据
	user, valid, err := validateCredentials(req.Username, req.Password)
	if err != nil {
		log.Printf("LoginHandler error for user %s: %v\n", req.Username, err)
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
	err = accounts.CreateLoginDevice(user.ID, deviceID, refreshToken, refreshTokenExpiry)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to store device token")
		return
	}

	common.RespondSuccessMessage(context, "Login successful", loginResponse{
		AccessToken:        "Bearer " + accessToken,
		AccessTokenExpiry:  accessTokenExpiry.Unix(),
		RefreshToken:       refreshToken,
		RefreshTokenExpiry: refreshTokenExpiry.Unix(),
		DeviceID:           deviceID,
	})
}

// RefreshTokenHandler Refresh token authentication
func RefreshTokenHandler(context *gin.Context) {
	var req refreshRuestBody
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}
	var deviceID = req.DeviceID

	if req.GrantType != "refresh_token" {
		common.RespondError(context, http.StatusBadRequest, "Invalid grant_type")
		return
	}
	if req.DeviceID == "" {
		common.RespondError(context, http.StatusBadRequest, "Device ID is required")
		return
	}

	// 查询设备信息与用户信息
	device, err := accounts.GetDeviceByRefreshTokenAndDeviceID(req.RefreshToken, req.DeviceID)
	if err != nil {
		common.RespondError(context, http.StatusUnauthorized, "User associated with token not found")
		return
	}
	user, err := accounts.GetUserByUserID(device.UserID)
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
	err = accounts.RotateRefreshToken(user.ID, device.DeviceID, newRefreshToken, newRefreshTokenExpiry)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to update device token")
		return
	}

	accessToken, accessTokenExpiry, err := GenerateTokens(user.Username, user.ID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to generate new access tokens")
		return
	}

	common.RespondSuccessMessage(context, "Refresh token successful", loginResponse{
		AccessToken:        "Bearer " + accessToken,
		AccessTokenExpiry:  accessTokenExpiry.Unix(),
		RefreshToken:       newRefreshToken,
		RefreshTokenExpiry: newRefreshTokenExpiry.Unix(),
		DeviceID:           deviceID,
	})

}

// LogoutHandler user logout
func LogoutHandler(context *gin.Context) {
	var req logoutResponse
	if err := context.ShouldBindJSON(&req); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}

	err := accounts.DeleteDeviceByDeviceID(req.DeviceID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to delete device")
		return
	}

	common.RespondSuccessMessage(context, "Logout successful", logoutResponse{})
}

// validateCredentials Verify user credentials
func validateCredentials(username, password string) (*models.User, bool, error) {
	user, err := accounts.GetUserByUsername(username)
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
