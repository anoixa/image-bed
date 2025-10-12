package key

import (
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/repo/key"
	"github.com/gin-gonic/gin"
)

type apiTokenResponse struct {
	ID          uint       `json:"id"`
	IsActive    bool       `json:"is_active"`
	Hash        string     `json:"hash"`
	Prefix      string     `json:"prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	Description string     `json:"description"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

func GetToken(context *gin.Context) {
	userID := context.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(context, http.StatusUnauthorized, "Invalid user session")
		return
	}

	apiTokens, err := key.GetAllApiTokensByUser(userID)
	if err != nil {
		log.Printf("Failed to get API tokens for user %d: %v", userID, err)

		common.RespondError(context, http.StatusInternalServerError, "Failed to retrieve API tokens due to an internal error.")
		return
	}

	responseDTOs := make([]apiTokenResponse, 0, len(apiTokens))
	for _, tokenModel := range apiTokens {
		responseDTOs = append(responseDTOs, apiTokenResponse{
			ID:          tokenModel.ID,
			Hash:        tokenModel.Token,
			Prefix:      tokenModel.TokenPrefix,
			IsActive:    tokenModel.IsActive,
			Description: tokenModel.Description,
			CreatedAt:   tokenModel.CreatedAt,
			LastUsedAt:  tokenModel.LastUsedAt,
		})
	}

	sort.Slice(responseDTOs, func(i, j int) bool {
		return responseDTOs[i].ID < responseDTOs[j].ID
	})

	totalCount := len(apiTokens)
	responsePayload := gin.H{
		"total_count": totalCount,
		"tokens":      responseDTOs,
	}

	common.RespondSuccess(context, responsePayload)
}
