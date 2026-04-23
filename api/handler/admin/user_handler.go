package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/admin"
	"github.com/gin-gonic/gin"
)

// UserHandler 管理员用户管理处理器
type UserHandler struct {
	svc *admin.UserService
}

// NewUserHandler 创建用户管理处理器
func NewUserHandler(svc *admin.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateRoleRequest 更新角色请求
type UpdateRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=admin user"`
}

// UpdateStatusRequest 更新状态请求
type UpdateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active disabled"`
}

// ListUsers 获取用户列表
func (h *UserHandler) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	users, total, err := h.svc.ListUsers(page, pageSize)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to list users")
		return
	}

	common.RespondSuccess(c, gin.H{
		"users":     users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// CreateUser 创建用户
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	user, generatedPassword, err := h.svc.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		switch {
		case errors.Is(err, admin.ErrUsernameEmpty):
			common.RespondError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, admin.ErrUsernameExists):
			common.RespondError(c, http.StatusConflict, err.Error())
		case errors.Is(err, admin.ErrPasswordTooShort):
			common.RespondError(c, http.StatusBadRequest, err.Error())
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to create user")
		}
		return
	}

	data := gin.H{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
		"status":   user.Status,
	}
	if generatedPassword != "" {
		data["password"] = generatedPassword
	}

	common.RespondSuccess(c, data)
}

// UpdateRole 更新用户角色
func (h *UserHandler) UpdateRole(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.svc.UpdateRole(uint(id), req.Role); err != nil {
		switch {
		case errors.Is(err, admin.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, admin.ErrLastAdmin):
			common.RespondError(c, http.StatusBadRequest, err.Error())
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to update role")
		}
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Role updated successfully"})
}

// UpdateStatus 更新用户状态
func (h *UserHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Cannot disable self
	currentUserID := c.GetUint(middleware.ContextUserIDKey)
	if uint(id) == currentUserID && req.Status == models.UserStatusDisabled {
		common.RespondError(c, http.StatusBadRequest, "Cannot disable yourself")
		return
	}

	if err := h.svc.UpdateStatus(uint(id), req.Status); err != nil {
		switch {
		case errors.Is(err, admin.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, admin.ErrLastAdmin):
			common.RespondError(c, http.StatusBadRequest, err.Error())
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to update status")
		}
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Status updated successfully"})
}

// ResetPassword 重置用户密码
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	newPassword, err := h.svc.ResetPassword(uint(id))
	if err != nil {
		if errors.Is(err, admin.ErrUserNotFound) {
			common.RespondError(c, http.StatusNotFound, err.Error())
		} else {
			common.RespondError(c, http.StatusInternalServerError, "Failed to reset password")
		}
		return
	}

	common.RespondSuccess(c, gin.H{
		"password": newPassword,
	})
}

// DeleteUser 删除用户
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Cannot delete self
	currentUserID := c.GetUint(middleware.ContextUserIDKey)
	if uint(id) == currentUserID {
		common.RespondError(c, http.StatusBadRequest, "Cannot delete yourself")
		return
	}

	if err := h.svc.DeleteUser(uint(id)); err != nil {
		switch {
		case errors.Is(err, admin.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, admin.ErrLastAdmin):
			common.RespondError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, admin.ErrUserHasOwnedData):
			common.RespondError(c, http.StatusConflict, err.Error())
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to delete user")
		}
		return
	}

	common.RespondSuccess(c, gin.H{"message": "User deleted successfully"})
}
