package images

import (
	"math"
	"net/http"
	"sort"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

type ImageDTO struct {
	ID           uint   `json:"id"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	OriginalName string `json:"original_name"`
	FileSize     int64  `json:"file_size"`
	MimeType     string `json:"mime_type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	IsPublic     bool   `json:"is_public"`
	CreatedAt    int64  `json:"created_at"`
}

type ImageRequestBody struct {
	StorageType string `json:"storage_type"`
	Identifier  string `json:"identifier"`
	Search      string `json:"search"`
	AlbumID     *uint  `json:"album_id"`
	StartTime   int64  `json:"start_time"`  // Unix时间戳（毫秒）
	EndTime     int64  `json:"end_time"`    // Unix时间戳（毫秒）

	Page  int `json:"page" binding:"required"`
	Limit int `json:"limit" binding:"required"`
}

type ImageListResponse struct {
	Images     []*ImageDTO `json:"images"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"total_pages"`
}

// ListImages 获取图片列表
func (h *Handler) ListImages(c *gin.Context) {
	var body ImageRequestBody

	if err := c.ShouldBindJSON(&body); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	page, limit := body.Page, body.Limit
	if body.Page <= 0 {
		page = 1
	}
	if body.Limit <= 0 {
		limit = 10
	}

	// 限制最大分页数量
	const maxLimit = 100
	if limit > maxLimit {
		limit = maxLimit
	}

	result, err := h.imageService.ListImages(body.StorageType, body.Identifier, body.Search, body.AlbumID, body.StartTime, body.EndTime, page, limit, int(userID))
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get image list")
		return
	}

	imageListDTO := h.toImageDTOs(result.Images)
	sort.Slice(imageListDTO, func(i, j int) bool {
		return imageListDTO[i].ID < imageListDTO[j].ID
	})

	common.RespondSuccess(c, ImageListResponse{
		Images:     imageListDTO,
		Total:      result.Total,
		Page:       body.Page,
		Limit:      body.Limit,
		TotalPages: int(math.Ceil(float64(result.Total) / float64(body.Limit))),
	})
}

func (h *Handler) toImageDTO(image *models.Image) *ImageDTO {
	if image == nil {
		return nil
	}

	imageUrl := utils.BuildImageURL(h.baseURL, image.Identifier)
	thumbnailUrl := utils.BuildThumbnailURL(h.baseURL, image.Identifier)

	return &ImageDTO{
		ID:           image.ID,
		URL:          imageUrl,
		ThumbnailURL: thumbnailUrl,
		OriginalName: image.OriginalName,
		FileSize:     image.FileSize,
		MimeType:     image.MimeType,
		Width:        image.Width,
		Height:       image.Height,
		IsPublic:     image.IsPublic,
		CreatedAt:    image.CreatedAt.Unix(),
	}
}

func (h *Handler) toImageDTOs(images []*models.Image) []*ImageDTO {
	dtos := make([]*ImageDTO, len(images))
	for i, image := range images {
		dtos[i] = h.toImageDTO(image)
	}
	return dtos
}
