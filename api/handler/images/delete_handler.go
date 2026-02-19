package images

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DeleteRequestBody struct {
	ImageID []string `json:"identifier" binding:"required"`
}

// DeleteImages 批量删除图片
func (h *Handler) DeleteImages(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session")
		return
	}

	var requestBody DeleteRequestBody
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body. 'identifier' field with a list of strings is required.")
		return
	}

	if len(requestBody.ImageID) == 0 {
		common.RespondError(c, http.StatusBadRequest, "No image identifiers provided for deletion.")
		return
	}

	// 获取图片信息以便级联删除变体
	ctx := c.Request.Context()
	var imagesToDelete []*models.Image
	for _, identifier := range requestBody.ImageID {
		img, err := h.repo.GetImageByIdentifier(identifier)
		if err == nil && img != nil && img.UserID == userID {
			imagesToDelete = append(imagesToDelete, img)
		}
	}

	for _, img := range imagesToDelete {
		h.deleteVariantsForImage(ctx, img)
	}

	affectedCount, err := h.repo.DeleteImagesByIdentifiersAndUser(requestBody.ImageID, userID)
	if err != nil {

		log.Printf("Failed to delete image records from database, but files have been removed: %v", err)
	}

	// 清除缓存
	for _, imageID := range requestBody.ImageID {
		if err := h.cacheHelper.DeleteCachedImage(ctx, imageID); err != nil {
			log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(imageID), err)
		}
		if err := h.cacheHelper.DeleteCachedImageData(ctx, imageID); err != nil {
			log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(imageID), err)
		}
	}

	common.RespondSuccessMessage(c, "Delete request processed successfully.", gin.H{"deleted_count": affectedCount})
}

// DeleteSingleImage 删除单张图片
func (h *Handler) DeleteSingleImage(c *gin.Context) {
	userID := c.GetUint(middleware.ContextUserIDKey)
	if userID == 0 {
		common.RespondError(c, http.StatusUnauthorized, "Invalid user session")
		return
	}

	imageIdentifier := c.Param("identifier")
	if imageIdentifier == "" {
		common.RespondError(c, http.StatusBadRequest, "Image identifier is required.")
		return
	}

	// 获取图片信息以便级联删除变体
	ctx := c.Request.Context()
	img, err := h.repo.GetImageByIdentifier(imageIdentifier)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get image info.")
		return
	}

	err = h.repo.DeleteImageByIdentifierAndUser(imageIdentifier, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.RespondError(c, http.StatusNotFound, "Image not found or you do not have permission to delete it.")
			return
		}
		common.RespondError(c, http.StatusInternalServerError, "Failed to delete the image due to an internal error.")
		return
	}

	// 级联删除变体
	if img != nil && img.UserID == userID {
		h.deleteVariantsForImage(ctx, img)
	}

	// 清除缓存
	if err := h.cacheHelper.DeleteCachedImage(ctx, imageIdentifier); err != nil {
		log.Printf("Failed to delete cache for image %s: %v", utils.SanitizeLogMessage(imageIdentifier), err)
	}
	if err := h.cacheHelper.DeleteCachedImageData(ctx, imageIdentifier); err != nil {
		log.Printf("Failed to delete image data cache for image %s: %v", utils.SanitizeLogMessage(imageIdentifier), err)
	}

	common.RespondSuccessMessage(c, "Image deleted successfully", nil)
}

// deleteVariantsForImage 删除图片的所有变体（文件、缓存、数据库记录）
func (h *Handler) deleteVariantsForImage(ctx context.Context, img *models.Image) {
	// 获取所有变体
	variants, err := h.variantRepo.GetVariantsByImageID(img.ID)
	if err != nil {
		log.Printf("Failed to get variants for image %d: %v", img.ID, err)
		return
	}

	// 删除每个变体的文件和缓存
	for _, variant := range variants {
		// 跳过未完成的变体（没有实际文件）
		if variant.Identifier == "" || variant.Status != models.VariantStatusCompleted {
			continue
		}

		// 删除文件
		if err := storage.GetDefault().DeleteWithContext(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete variant file %s: %v", variant.Identifier, err)
		}

		// 清除变体缓存
		if err := h.cacheHelper.DeleteCachedImageData(ctx, variant.Identifier); err != nil {
			log.Printf("Failed to delete cache for variant %s: %v", utils.SanitizeLogMessage(variant.Identifier), err)
		}
	}

	// 删除数据库中的变体记录
	if err := h.variantRepo.DeleteByImageID(img.ID); err != nil {
		log.Printf("Failed to delete variant records for image %d: %v", img.ID, err)
	}
}
