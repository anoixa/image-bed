package albums

import (
	"context"
	"log"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

type createAlbumRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Description string `json:"description" binding:"max=255"`
}

// CreateAlbumResponse 创建相册响应
type CreateAlbumResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// CreateAlbumHandler 创建相册
// @Summary      Create album
// @Description  Create a new album with name and optional description
// @Tags         albums
// @Accept       json
// @Produce      json
// @Param        request  body      createAlbumRequest  true  "Album creation request"
// @Success      200      {object}  common.Response{data=CreateAlbumResponse}  "Album created successfully"
// @Failure      400      {object}  common.Response  "Invalid request body"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /albums [post]
func (h *Handler) CreateAlbumHandler(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	var req createAlbumRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	album := models.Album{
		Name:        req.Name,
		Description: req.Description,
		UserID:      userID,
	}

	if err := h.svc.CreateAlbum(&album); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create albums.")
		return
	}

	// 清除用户相册列表缓存
	utils.SafeGo(func() {
		ctx := context.Background()
		if err := h.cacheHelper.DeleteCachedAlbumList(ctx, userID); err != nil {
			log.Printf("Failed to delete album list cache for user %d: %v", userID, err)
		}
	})

	resp := CreateAlbumResponse{
		ID:          album.ID,
		Name:        album.Name,
		Description: album.Description,
		CreatedAt:   album.CreatedAt.Unix(),
		UpdatedAt:   album.UpdatedAt.Unix(),
	}
	common.RespondSuccess(c, resp)
}
