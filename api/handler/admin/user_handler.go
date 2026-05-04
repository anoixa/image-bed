package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

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

type UserSummary struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ListUsersResponse struct {
	Users    []UserSummary `json:"users"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

type CreateUserResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	Password string `json:"password,omitempty"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type ResetPasswordResponse struct {
	Password string `json:"password"`
}

// ListUsers
// @Summary      List users
// @Description  Get paginated user list for administration
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        page       query     int  false  "Page number"       minimum(1)
// @Param        page_size  query     int  false  "Page size"         minimum(1) maximum(100)
// @Success      200        {object}  common.Response{data=ListUsersResponse}  "User list"
// @Failure      401        {object}  common.Response                              "Unauthorized"
// @Failure      403        {object}  common.Response                              "Forbidden"
// @Failure      500        {object}  common.Response                              "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users [get]
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

// CreateUser
// @Summary      Create user
// @Description  Create a new user. If password is omitted, backend auto-generates one and returns it once.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      CreateUserRequest                           true  "Create user request"
// @Success      200      {object}  common.Response{data=CreateUserResponse}   "User created"
// @Failure      400      {object}  common.Response                            "Validation error"
// @Failure      401      {object}  common.Response                            "Unauthorized"
// @Failure      403      {object}  common.Response                            "Forbidden"
// @Failure      409      {object}  common.Response                            "Username already exists"
// @Failure      500      {object}  common.Response                            "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users [post]
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

// UpdateRole
// @Summary      Update user role
// @Description  Change a user's role between admin and user
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id       path      int                true  "User ID"
// @Param        request  body      UpdateRoleRequest  true  "Role update request"
// @Success      200      {object}  common.Response{data=MessageResponse}  "Role updated"
// @Failure      400      {object}  common.Response                         "Validation error or last admin protection"
// @Failure      401      {object}  common.Response                         "Unauthorized"
// @Failure      403      {object}  common.Response                         "Forbidden"
// @Failure      404      {object}  common.Response                         "User not found"
// @Failure      500      {object}  common.Response                         "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/role [put]
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

// UpdateStatus
// @Summary      Update user status
// @Description  Change a user's status between active and disabled
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id       path      int                  true  "User ID"
// @Param        request  body      UpdateStatusRequest  true  "Status update request"
// @Success      200      {object}  common.Response{data=MessageResponse}  "Status updated"
// @Failure      400      {object}  common.Response                         "Validation error, disable self, or last admin protection"
// @Failure      401      {object}  common.Response                         "Unauthorized"
// @Failure      403      {object}  common.Response                         "Forbidden"
// @Failure      404      {object}  common.Response                         "User not found"
// @Failure      500      {object}  common.Response                         "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/status [put]
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

// ResetPassword
// @Summary      Reset user password
// @Description  Reset a user's password and revoke all of their active sessions
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  common.Response{data=ResetPasswordResponse}  "Password reset"
// @Failure      400  {object}  common.Response                               "Invalid user ID"
// @Failure      401  {object}  common.Response                               "Unauthorized"
// @Failure      403  {object}  common.Response                               "Forbidden"
// @Failure      404  {object}  common.Response                               "User not found"
// @Failure      500  {object}  common.Response                               "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/reset-password [post]
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

// DeleteUser
// @Summary      Delete user
// @Description  Delete a user when they do not own images or albums
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  common.Response{data=MessageResponse}  "User deleted"
// @Failure      400  {object}  common.Response                         "Invalid user ID, delete self, or last admin protection"
// @Failure      401  {object}  common.Response                         "Unauthorized"
// @Failure      403  {object}  common.Response                         "Forbidden"
// @Failure      404  {object}  common.Response                         "User not found"
// @Failure      409  {object}  common.Response                         "User still owns data"
// @Failure      500  {object}  common.Response                         "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id} [delete]
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

// CreateOAuthInviteRequest is the request body for creating an OAuth invite.
type CreateOAuthInviteRequest struct {
	Provider  string  `json:"provider" binding:"required"`
	Subject   string  `json:"subject"`
	Email     string  `json:"email"`
	ExpiresAt *string `json:"expires_at"`
}

type oauthUserIdentitiesResponse struct {
	Identities []*models.UserIdentity `json:"identities"`
	Invites    []*models.OAuthInvite  `json:"invites"`
}

type createOAuthInviteResponse struct {
	ID        uint       `json:"id" example:"1"`
	Provider  string     `json:"provider" example:"github"`
	Subject   string     `json:"subject,omitempty" example:"123456"`
	Email     string     `json:"email,omitempty" example:"user@example.com"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// GetOAuthIdentities returns linked OAuth identities for a user.
// @Summary      Get user OAuth identities and invites
// @Description  Get linked OAuth identities and pending/used OAuth invites for a user.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  common.Response{data=oauthUserIdentitiesResponse}  "OAuth identity and invite list"
// @Failure      400  {object}  common.Response  "Invalid user ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Failed to get identities or invites"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/oauth-identities [get]
func (h *UserHandler) GetOAuthIdentities(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	identities, err := h.svc.GetUserIdentities(c.Request.Context(), uint(id))
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get identities")
		return
	}

	invites, err := h.svc.GetUserInvites(c.Request.Context(), uint(id))
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get invites")
		return
	}

	common.RespondSuccess(c, gin.H{
		"identities": identities,
		"invites":    invites,
	})
}

// CreateOAuthInvite creates an OAuth invite for a user.
// @Summary      Create OAuth invite
// @Description  Create an invite that allows a specific OAuth subject or verified email to log in as the target user.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id       path      int                       true  "User ID"
// @Param        request  body      CreateOAuthInviteRequest  true  "OAuth invite"
// @Success      200      {object}  common.Response{data=createOAuthInviteResponse}  "OAuth invite created"
// @Failure      400      {object}  common.Response  "Invalid request"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      404      {object}  common.Response  "User not found"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/oauth-invites [post]
func (h *UserHandler) CreateOAuthInvite(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req CreateOAuthInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			common.RespondError(c, http.StatusBadRequest, "Invalid expires_at format, use RFC3339")
			return
		}
		expiresAt = &t
	}

	adminID := c.GetUint(middleware.ContextUserIDKey)
	invite, err := h.svc.CreateOAuthInvite(c.Request.Context(), uint(id), req.Provider, req.Subject, req.Email, expiresAt, adminID)
	if err != nil {
		switch {
		case errors.Is(err, admin.ErrUserNotFound):
			common.RespondError(c, http.StatusNotFound, err.Error())
		default:
			common.RespondError(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	common.RespondSuccess(c, gin.H{
		"id":         invite.ID,
		"provider":   invite.Provider,
		"subject":    invite.Subject,
		"email":      invite.Email,
		"expires_at": invite.ExpiresAt,
		"created_at": invite.CreatedAt,
	})
}

// DeleteOAuthInvite deletes an OAuth invite.
// @Summary      Delete OAuth invite
// @Description  Delete an OAuth invite by invite ID.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        id         path      int  true  "User ID"
// @Param        invite_id  path      int  true  "OAuth invite ID"
// @Success      200        {object}  common.Response  "Invite deleted"
// @Failure      400        {object}  common.Response  "Invalid invite ID"
// @Failure      401        {object}  common.Response  "Unauthorized"
// @Failure      404        {object}  common.Response  "Invite not found"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/users/{id}/oauth-invites/{invite_id} [delete]
func (h *UserHandler) DeleteOAuthInvite(c *gin.Context) {
	inviteID, err := strconv.ParseUint(c.Param("invite_id"), 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid invite ID")
		return
	}

	if err := h.svc.DeleteOAuthInvite(c.Request.Context(), uint(inviteID)); err != nil {
		common.RespondError(c, http.StatusNotFound, "Invite not found")
		return
	}

	common.RespondSuccess(c, gin.H{"message": "Invite deleted successfully"})
}
