package albums

import (
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetUserAlbums(userID uint, page, pageSize int) ([]*models.Album, int64, error) {
	var albums []*models.Album
	var total int64
	db := dbcore.GetDBInstance().Model(&models.Album{}).Where("user_id = ?", userID)

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&albums).Error
	return albums, total, err
}

func GetAlbumWithImagesByID(albumID, userID uint) (*models.Album, error) {
	var album models.Album
	err := dbcore.GetDBInstance().Preload("Images").First(&album, "id = ? AND user_id = ?", albumID, userID).Error
	return &album, err
}

func AddImageToAlbum(albumID, userID uint, image *models.Image) error {
	return dbcore.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("albums with ID %d not found or access denied", albumID)
			}
			return err
		}

		return tx.Model(&album).Association("Images").Append(image)
	})
}

func RemoveImageFromAlbum(albumID, userID uint, image *models.Image) error {
	return dbcore.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("albums with ID %d not found or access denied", albumID)
			}
			return err
		}

		return tx.Model(&album).Association("Images").Delete(image)
	})
}

func CreateAlbum(album *models.Album) error {
	return dbcore.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&album).Error; err != nil {
			return fmt.Errorf("failed to create albums in transaction: %w", err)
		}
		return nil
	})
}

func DeleteAlbum(albumID, userID uint) error {
	return dbcore.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("albums with ID %d not found or access denied", albumID)
			}
			return err
		}

		if err := tx.Model(&album).Association("Images").Clear(); err != nil {
			return fmt.Errorf("failed to clear image associations for albums %d: %w", albumID, err)
		}

		if err := tx.Delete(&album).Error; err != nil {
			return fmt.Errorf("failed to delete albums %d: %w", albumID, err)
		}

		return nil
	})
}
