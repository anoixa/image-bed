package key

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handler) DisableToken(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	tokenIDStr := context.Param("id")
	tokenID64, err := strconv.ParseUint(tokenIDStr, 10, 32)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid token ID format.")
		return
	}
	tokenID := uint(tokenID64)

	err = h.svc.DisableApiToken(tokenID, userID)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(context, http.StatusNotFound, "API token not found or you do not have permission to modify it.")
			return
		}

		log.Printf("Failed to disable API token %d for user %d: %v", tokenID, userID, err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to disable the API token due to an internal error.")
		return
	}

	common.RespondSuccessMessage(context, "API token has been successfully disabled.", nil)
}

func (h *Handler) RevokeToken(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	tokenIDStr := context.Param("id")
	tokenID64, err := strconv.ParseUint(tokenIDStr, 10, 32)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid token ID format.")
		return
	}
	tokenID := uint(tokenID64)

	err = h.svc.RevokeApiToken(tokenID, userID)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(context, http.StatusNotFound, "API token not found or you do not have permission to revoke it.")
			return
		}

		log.Printf("Failed to revoke API token %d for user %d: %v", tokenID, userID, err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to revoke the API token due to an internal error.")
		return
	}

	common.RespondSuccessMessage(context, "API token has been successfully revoked", nil)
}

func (h *Handler) EnableToken(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	tokenIDStr := context.Param("id")
	tokenID64, err := strconv.ParseUint(tokenIDStr, 10, 32)
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Invalid token ID format.")
		return
	}
	tokenID := uint(tokenID64)

	err = h.svc.EnableApiToken(tokenID, userID)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(context, http.StatusNotFound, "API token not found or you do not have permission to modify it.")
			return
		}

		log.Printf("Failed to enable API token %d for user %d: %v", tokenID, userID, err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to enable the API token due to an internal error.")
		return
	}

	common.RespondSuccessMessage(context, "API token has been successfully enabled.", nil)
}
