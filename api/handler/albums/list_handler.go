package albums

import (
	"math"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/gin-gonic/gin"
)

// AlbumDTO 相册响应数据
type AlbumDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ImageCount  int64  `json:"image_count"`
	CoverURL    string `json:"cover_url,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// ListAlbumsResponse 相册列表响应
type ListAlbumsResponse struct {
	Albums     []*AlbumDTO `json:"albums"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"total_pages"`
}

// ListAlbumsRequest 相册列表请求
type ListAlbumsRequest struct {
	Page  int `form:"page" json:"page" binding:"required,min=1"`
	Limit int `form:"limit" json:"limit" binding:"required,min=1,max=100"`
}

// ListAlbumsHandler 获取相册列表
func (h *Handler) ListAlbumsHandler(c *gin.Context) {
	var req ListAlbumsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request parameters")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	albums, total, err := h.repo.GetUserAlbums(userID, req.Page, req.Limit)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get albums")
		return
	}

	albumDTOs := make([]*AlbumDTO, len(albums))
	for i, info := range albums {
		albumDTOs[i] = &AlbumDTO{
			ID:          info.Album.ID,
			Name:        info.Album.Name,
			Description: info.Album.Description,
			ImageCount:  info.ImageCount,
			CoverURL:    info.CoverURL,
			CreatedAt:   info.Album.CreatedAt.Unix(),
			UpdatedAt:   info.Album.UpdatedAt.Unix(),
		}
	}

	common.RespondSuccess(c, ListAlbumsResponse{
		Albums:     albumDTOs,
		Total:      total,
		Page:       req.Page,
		Limit:      req.Limit,
		TotalPages: int(math.Ceil(float64(total) / float64(req.Limit))),
	})
}
