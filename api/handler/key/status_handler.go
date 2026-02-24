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


func (h *Handler) DisableToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.DisableApiToken, "disable", "disabled")
}

func (h *Handler) RevokeToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.RevokeApiToken, "revoke", "revoked")
}

func (h *Handler) EnableToken(c *gin.Context) {
	h.executeTokenAction(c, h.svc.EnableApiToken, "enable", "enabled")
}