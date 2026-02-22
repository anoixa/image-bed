package key

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

type req struct {
	Description string `json:"description"`
}

// CreateStaticToken 创建新的static token
func (h *Handler) CreateStaticToken(context *gin.Context) {
	var requestBody req
	if err := context.ShouldBindJSON(&requestBody); err != nil {
		if err != io.EOF {
			common.RespondError(context, http.StatusBadRequest, "Invalid JSON format: "+err.Error())
			return
		}
	}

	userID := context.GetUint(middleware.ContextUserIDKey)

	randomToken, err := utils.GenerateRandomToken(64)
	tokenPrefix := randomToken[:12]
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, err.Error())
		return
	}
	hasher := sha256.New()
	hasher.Write([]byte(randomToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	token := models.ApiToken{
		UserID:      userID,
		Token:       hashedToken,
		TokenPrefix: tokenPrefix,
		Description: requestBody.Description,
		IsActive:    true,
	}

	err = h.svc.CreateKey(&token)

	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondSuccessMessage(context, "success create static token", gin.H{
		"token": "ApiKey " + randomToken,
		"hash":  hashedToken,
	})
}
