package user

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/internal/user"
	"github.com/gin-gonic/gin"
)

// Handler 用户处理器
type Handler struct {
	service *user.Service
}

// NewHandler 创建新的用户处理器
func NewHandler(service *user.Service) *Handler {
	return &Handler{
		service: service,
	}
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ChangePasswordResponse 修改密码响应
type ChangePasswordResponse struct {
	Message string `json:"message"`
}

// ChangePassword 修改密码
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
		switch err.Error() {
		case "user not found":
			common.RespondError(c, http.StatusNotFound, "User not found")
		case "invalid old password":
			common.RespondError(c, http.StatusUnauthorized, "Invalid old password")
		case "new password cannot be the same as old password":
			common.RespondError(c, http.StatusBadRequest, "New password cannot be the same as old password")
		default:
			common.RespondError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	common.RespondSuccess(c, ChangePasswordResponse{
		Message: "Password changed successfully",
	})
}
