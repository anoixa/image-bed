package user

import (
	"errors"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/internal/user"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *user.Service
}

func NewHandler(service *user.Service) *Handler {
	return &Handler{
		service: service,
	}
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=1024"`
}

type ChangePasswordResponse struct {
	Message string `json:"message"`
}

type CurrentUserResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

// GetCurrentUser
// @Summary      获取当前用户信息
// @Description  返回当前 JWT 登录用户的基础资料
// @Tags         auth
// @Produce      json
// @Success      200  {object}  common.Response{data=CurrentUserResponse}
// @Failure      401  {object}  common.Response  "未认证"
// @Failure      404  {object}  common.Response  "用户不存在"
// @Failure      500  {object}  common.Response  "服务器内部错误"
// @Security     ApiKeyAuth
// @Router       /api/auth/me [get]
func (h *Handler) GetCurrentUser(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	currentUser, err := h.service.GetCurrentUser(userID)
	if err != nil {
		switch {
		case errors.Is(err, user.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, "User not found")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to get current user")
		}
		return
	}

	common.RespondSuccess(c, CurrentUserResponse{
		ID:       currentUser.ID,
		Username: currentUser.Username,
		Role:     currentUser.Role,
		Status:   currentUser.Status,
	})
}

// ChangePassword
// @Summary      修改用户密码
// @Description  验证旧密码并更新为新密码
// @Tags         user
// @Accept       json
// @Produce      json
// @Param        request  body      ChangePasswordRequest  true  "修改密码请求"
// @Success      200      {object}  common.Response{data=ChangePasswordResponse}
// @Failure      400      {object}  common.Response  "请求参数错误"
// @Failure      401      {object}  common.Response  "旧密码错误"
// @Failure      500      {object}  common.Response  "服务器内部错误"
// @Security     ApiKeyAuth
// @Router       /api/v1/user/password [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	err := h.service.ChangePassword(user.ChangePasswordRequest{
		UserID:      userID,
		OldPassword: req.OldPassword,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		switch {
		case errors.Is(err, user.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, "User not found")
		case errors.Is(err, user.ErrInvalidOldPassword):
			common.RespondError(c, http.StatusUnauthorized, "Invalid old password")
		case errors.Is(err, user.ErrSamePassword):
			common.RespondError(c, http.StatusBadRequest, "New password cannot be the same as old password")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to change password")
		}
		return
	}

	common.RespondSuccess(c, ChangePasswordResponse{
		Message: "Password changed successfully",
	})
}

type twoFAStatusResponse struct {
	Enabled bool `json:"enabled"`
}

type twoFASetupResponse struct {
	Enabled bool   `json:"enabled"`
	URI     string `json:"uri"`
	Secret  string `json:"secret"`
}

type twoFASetupRequest struct {
	CurrentPassword string `json:"current_password"`
	CurrentCode     string `json:"current_code"`
}

type twoFACodeRequest struct {
	Code string `json:"code" binding:"required,len=6"`
}

// Get2FAStatus
// @Summary      获取 2FA 状态
// @Description  返回当前用户的 TOTP 两步验证状态
// @Tags         user
// @Produce      json
// @Success      200  {object}  common.Response{data=twoFAStatusResponse}
// @Failure      401  {object}  common.Response
// @Failure      500  {object}  common.Response
// @Security     ApiKeyAuth
// @Router       /api/v1/user/2fa [get]
func (h *Handler) Get2FAStatus(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	enabled, err := h.service.Get2FAStatus(userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get 2FA status")
		return
	}

	common.RespondSuccess(c, twoFAStatusResponse{Enabled: enabled})
}

// Setup2FA
// @Summary      设置 2FA
// @Description  生成 TOTP secret 和 URI，返回给前端渲染 QR 码。不立即启用，需调用 enable 接口确认。
// @Tags         user
// @Accept       json
// @Produce      json
// @Param        request  body      twoFASetupRequest  true  "Current password or current TOTP code"
// @Success      200  {object}  common.Response{data=twoFASetupResponse}
// @Failure      400  {object}  common.Response  "2FA already enabled"
// @Failure      401  {object}  common.Response
// @Failure      500  {object}  common.Response
// @Security     ApiKeyAuth
// @Router       /api/v1/user/2fa/setup [post]
func (h *Handler) Setup2FA(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var req twoFASetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	result, err := h.service.Setup2FA(user.Setup2FARequest{
		UserID:          userID,
		CurrentPassword: req.CurrentPassword,
		CurrentCode:     req.CurrentCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, user.ErrInvalidOldPassword):
			common.RespondError(c, http.StatusUnauthorized, "Invalid current password")
		case errors.Is(err, user.ErrInvalid2FACode):
			common.RespondError(c, http.StatusUnauthorized, "Invalid 2FA code")
		case errors.Is(err, user.Err2FANotSetup):
			common.RespondError(c, http.StatusBadRequest, "2FA not enabled")
		case errors.Is(err, user.Err2FAReplay):
			common.RespondError(c, http.StatusUnauthorized, "2FA code already used")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to setup 2FA")
		}
		return
	}

	common.RespondSuccess(c, twoFASetupResponse{
		Enabled: result.Enabled,
		URI:     result.URI,
		Secret:  result.Secret,
	})
}

// Enable2FA
// @Summary      启用 2FA
// @Description  验证 TOTP code，确认启用两步验证
// @Tags         user
// @Accept       json
// @Produce      json
// @Param        request  body      twoFACodeRequest  true  "TOTP code"
// @Success      200      {object}  common.Response
// @Failure      400      {object}  common.Response  "Invalid code or 2FA not set up"
// @Failure      401      {object}  common.Response
// @Security     ApiKeyAuth
// @Router       /api/v1/user/2fa/enable [post]
func (h *Handler) Enable2FA(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	var req twoFACodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request: code must be 6 digits")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	err := h.service.Enable2FA(userID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, user.Err2FAAlreadyEnabled):
			common.RespondError(c, http.StatusBadRequest, "2FA already enabled")
		case errors.Is(err, user.Err2FANotSetup):
			common.RespondError(c, http.StatusBadRequest, "2FA not set up, call setup first")
		case errors.Is(err, user.ErrInvalid2FACode):
			common.RespondError(c, http.StatusUnauthorized, "Invalid 2FA code")
		case errors.Is(err, user.Err2FAReplay):
			common.RespondError(c, http.StatusUnauthorized, "2FA code already used")
		case errors.Is(err, user.Err2FAServiceDisabled):
			common.RespondError(c, http.StatusInternalServerError, "2FA service not initialized")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to enable 2FA")
		}
		return
	}

	common.RespondSuccessMessage(c, "2FA enabled", nil)
}

// Disable2FA
// @Summary      关闭 2FA
// @Description  验证当前 TOTP code 后关闭两步验证
// @Tags         user
// @Accept       json
// @Produce      json
// @Param        request  body      twoFACodeRequest  true  "TOTP code"
// @Success      200      {object}  common.Response
// @Failure      400      {object}  common.Response
// @Failure      401      {object}  common.Response
// @Security     ApiKeyAuth
// @Router       /api/v1/user/2fa/disable [post]
func (h *Handler) Disable2FA(c *gin.Context) {
	if h.service == nil {
		common.RespondError(c, http.StatusInternalServerError, "User service not initialized")
		return
	}

	var req twoFACodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request: code must be 6 digits")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "User not authenticated")
		return
	}

	err := h.service.Disable2FA(userID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, user.Err2FANotSetup):
			common.RespondError(c, http.StatusBadRequest, "2FA not enabled")
		case errors.Is(err, user.ErrInvalid2FACode):
			common.RespondError(c, http.StatusUnauthorized, "Invalid 2FA code")
		case errors.Is(err, user.Err2FAReplay):
			common.RespondError(c, http.StatusUnauthorized, "2FA code already used")
		case errors.Is(err, user.Err2FAServiceDisabled):
			common.RespondError(c, http.StatusInternalServerError, "2FA service not initialized")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to disable 2FA")
		}
		return
	}

	common.RespondSuccessMessage(c, "2FA disabled", nil)
}
