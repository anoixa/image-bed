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
