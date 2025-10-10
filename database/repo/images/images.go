package images

import (
	"fmt"

	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

func SaveImage(image *models.Image) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Create(&image).Error

		if err != nil {
			return fmt.Errorf("failed to create images in transaction: %w", err)
		}
		return nil
	})

	return err
}

// GetImageByHash finds a single image by its SHA-256 hash.
func GetImageByHash(hash string) (*models.Image, error) {
	instance := dbcore.GetDBInstance()
	var image models.Image

	err := instance.Where("file_hash = ?", hash).First(&image).Error

	if err != nil {
		return nil, err
	}

	return &image, nil
}

func GetImageByIdentifier(identifier string) (*models.Image, error) {
	instance := dbcore.GetDBInstance()
	var image models.Image
	result := instance.Where("identifier = ?", identifier).First(&image)
	return &image, result.Error
}

func DeleteImage(image *models.Image) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Delete(&image).Error
		if err != nil {
			return fmt.Errorf("failed to delete images in transaction: %w", err)
		}
		return err
	})
	return err
}

// GetImageList 获取图片列表
func GetImageList(StorageType, Identifier, search string, page, limit, userID int) ([]*models.Image, int64, error) {
	instance := dbcore.GetDBInstance()
	query := instance.Model(&models.Image{}).Where("user_id = ?", userID)

	if StorageType != "" {
		query = query.Where("storage_driver = ?", StorageType)
	}

	if Identifier != "" {
		query = query.Where("identifier = ?", Identifier)
	}

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("original_name ILIKE ? OR identifier ILIKE ? OR file_hash ILIKE ?", searchPattern, searchPattern, searchPattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []*models.Image{}, 0, nil
	}

	offset := (page - 1) * limit

	paginatedQuery := query.Order("id DESC").Offset(offset).Limit(limit)

	var images []*models.Image
	err := paginatedQuery.Find(&images).Error
	if err != nil {
		return nil, 0, err
	}

	return images, total, nil
}
