package albums

import (
	"context"
	"log"
	"math"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

// CachedAlbumList 缓存的相册列表数据结构
type CachedAlbumList struct {
	Albums []*AlbumDTO `json:"albums"`
	Total  int64       `json:"total"`
}

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

	// 尝试从缓存获取
	var cachedList CachedAlbumList
	if err := h.cacheHelper.GetCachedAlbumList(c.Request.Context(), userID, req.Page, req.Limit, &cachedList); err == nil {
		common.RespondSuccess(c, ListAlbumsResponse{
			Albums:     cachedList.Albums,
			Total:      cachedList.Total,
			Page:       req.Page,
			Limit:      req.Limit,
			TotalPages: int(math.Ceil(float64(cachedList.Total) / float64(req.Limit))),
		})
		return
	}

	albums, total, err := h.svc.GetUserAlbums(userID, req.Page, req.Limit)
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

	// 异步写入缓存
	utils.SafeGo(func() {
		ctx := context.Background()
		cacheData := CachedAlbumList{
			Albums: albumDTOs,
			Total:  total,
		}
		if err := h.cacheHelper.CacheAlbumList(ctx, userID, req.Page, req.Limit, cacheData); err != nil {
			log.Printf("Failed to cache album list for user %d: %v", userID, err)
		}
	})

	common.RespondSuccess(c, ListAlbumsResponse{
		Albums:     albumDTOs,
		Total:      total,
		Page:       req.Page,
		Limit:      req.Limit,
		TotalPages: int(math.Ceil(float64(total) / float64(req.Limit))),
	})
}
