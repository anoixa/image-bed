package images

import (
	"fmt"
	"gorm.io/gorm"
	"image-bed/database/dbcore"
	"image-bed/database/models"
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
