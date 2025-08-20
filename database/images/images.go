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
