package key

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TokenAction token 操作函数
type TokenAction func(tokenID, userID uint) error

// getUserID 从上下文获取用户ID
func getUserID(c *gin.Context) uint {
	return c.GetUint(middleware.ContextUserIDKey)
}

// parseTokenID 从路径参数解析 token ID
func parseTokenID(c *gin.Context) (uint, bool) {
	tokenIDStr := c.Param("id")
	tokenID64, err := strconv.ParseUint(tokenIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid token ID format.")
		return 0, false
	}
	return uint(tokenID64), true
}

// executeTokenAction 执行 token 操作的通用处理
func (h *Handler) executeTokenAction(c *gin.Context, action TokenAction, actionVerb, actionPast string) {
	userID := getUserID(c)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session.")
		return
	}

	tokenID, ok := parseTokenID(c)
	if !ok {
		return
	}

	if err := action(tokenID, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound,
				fmt.Sprintf("API token not found or you do not have permission to %s it.", actionVerb))
			return
		}

		log.Printf("Failed to %s API token %d for user %d: %v", actionVerb, tokenID, userID, err)
		common.RespondError(c, http.StatusInternalServerError,
			fmt.Sprintf("Failed to %s the API token due to an internal error.", actionVerb))
		return
	}

	common.RespondSuccessMessage(c,
		fmt.Sprintf("API token has been successfully %s.", actionPast), nil)
}

// DisableToken 禁用 API Key
// @Summary      Disable API key
// @Description  Temporarily disable an API key (can be re-enabled later)
// @Tags         keys
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "API Key ID"
// @Success      200  {object}  common.Response  "API key disabled successfully"
// @Failure      400  {object}  common.Response  "Invalid API key ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "API key not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /keys/{id}/disable [post]
func (h *Handler) DisableToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.DisableApiToken, "disable", "disabled")
}

// RevokeToken 撤销 API Key
// @Summary      Revoke API key
// @Description  Permanently revoke an API key (cannot be re-enabled)
// @Tags         keys
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "API Key ID"
// @Success      200  {object}  common.Response  "API key revoked successfully"
// @Failure      400  {object}  common.Response  "Invalid API key ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "API key not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /keys/{id}/revoke [post]
func (h *Handler) RevokeToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.RevokeApiToken, "revoke", "revoked")
}

// EnableToken 启用 API Key
// @Summary      Enable API key
// @Description  Re-enable a previously disabled API key
// @Tags         keys
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "API Key ID"
// @Success      200  {object}  common.Response  "API key enabled successfully"
// @Failure      400  {object}  common.Response  "Invalid API key ID"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      404  {object}  common.Response  "API key not found"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /keys/{id}/enable [post]
func (h *Handler) EnableToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.EnableApiToken, "enable", "enabled")
}
