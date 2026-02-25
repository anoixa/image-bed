package albums

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AlbumImageDTO 相册中的图片信息
type AlbumImageDTO struct {
	ID           uint   `json:"id"`
	URL          string `json:"url"`
	OriginalName string `json:"original_name"`
	FileSize     int64  `json:"file_size"`
	MimeType     string `json:"mime_type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CreatedAt    int64  `json:"created_at"`
}

// AlbumDetailResponse 相册详情响应
type AlbumDetailResponse struct {
	ID          uint             `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Images      []*AlbumImageDTO `json:"images"`
	ImageCount  int64            `json:"image_count"`
	CreatedAt   int64            `json:"created_at"`
	UpdatedAt   int64            `json:"updated_at"`
}

// GetAlbumDetailHandler 获取相册详情
func (h *Handler) GetAlbumDetailHandler(c *gin.Context) {
	// 获取相册 ID
	albumIDStr := c.Param("id")
	albumID, err := strconv.ParseUint(albumIDStr, 10, 32)
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid album ID format")
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	album, err := h.svc.GetAlbumWithImagesByID(uint(albumID), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Album not found")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to get album")
		return
	}

	images := make([]*AlbumImageDTO, len(album.Images))
	for i, img := range album.Images {
		images[i] = h.toAlbumImageDTO(img)
	}

	common.RespondSuccess(c, AlbumDetailResponse{
		ID:          album.ID,
		Name:        album.Name,
		Description: album.Description,
		Images:      images,
		ImageCount:  int64(len(album.Images)),
		CreatedAt:   album.CreatedAt.Unix(),
		UpdatedAt:   album.UpdatedAt.Unix(),
	})
}

func (h *Handler) toAlbumImageDTO(image *models.Image) *AlbumImageDTO {
	if image == nil {
		return nil
	}

	imageUrl := utils.BuildImageURL(h.baseURL, image.Identifier)

	return &AlbumImageDTO{
		ID:           image.ID,
		URL:          imageUrl,
		OriginalName: image.OriginalName,
		FileSize:     image.FileSize,
		MimeType:     image.MimeType,
		Width:        image.Width,
		Height:       image.Height,
		CreatedAt:    image.CreatedAt.Unix(),
	}
}
