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
