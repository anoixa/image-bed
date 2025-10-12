package images

import (
	"math"
	"net/http"
	"sort"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

type ImageDTO struct {
	ID           uint   `json:"id"`
	URL          string `json:"url"`
	OriginalName string `json:"original_name"`
	FileSize     int64  `json:"file_size"`
	MimeType     string `json:"mime_type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CreatedAt    int64  `json:"created_at"`
}

type ImageRequestBody struct {
	StorageType string `json:"storage_type"`
	Identifier  string `json:"identifier"`
	Search      string `json:"search"`

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

func ImageListHandler(context *gin.Context) {
	var body ImageRequestBody
	var page, limit int

	if err := context.ShouldBindJSON(&body); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}

	userID := context.GetUint(middleware.ContextUserIDKey)

	page, limit = body.Page, body.Limit
	if body.Page <= 0 {
		page = 1
	}
	if body.Limit <= 0 {
		limit = 10
	}

	list, total, err := images.GetImageList(body.StorageType, body.Identifier, body.Search, page, limit, int(userID))
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to get image list")
		return
	}

	imageListDTO := toImageDTOs(list)
	sort.Slice(imageListDTO, func(i, j int) bool {
		return imageListDTO[i].ID < imageListDTO[j].ID
	})

	common.RespondSuccess(context, ImageListResponse{
		Images:     imageListDTO,
		Total:      total,
		Page:       body.Page,
		Limit:      body.Limit,
		TotalPages: int(math.Ceil(float64(total) / float64(body.Limit))),
	})
}
func toImageDTO(image *models.Image) *ImageDTO {
	if image == nil {
		return nil
	}

	imageUrl := utils.BuildImageURL(image.Identifier)

	return &ImageDTO{
		ID:           image.ID,
		URL:          imageUrl,
		OriginalName: image.OriginalName,
		FileSize:     image.FileSize,
		MimeType:     image.MimeType,
		Width:        image.Width,
		Height:       image.Height,
		CreatedAt:    image.CreatedAt.Unix(), // 转换为 Unix 时间戳
	}
}

func toImageDTOs(images []*models.Image) []*ImageDTO {
	dtos := make([]*ImageDTO, len(images))
	for i, image := range images {
		dtos[i] = toImageDTO(image)
	}
	return dtos
}
