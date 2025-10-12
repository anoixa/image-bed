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

//func DeleteImagesByIdentifiers(identifiers []string) error {
//	if len(identifiers) == 0 {
//		return nil
//	}
//
//	err := dbcore.Transaction(func(tx *gorm.DB) error {
//		result := tx.Where("identifier IN ?", identifiers).Delete(&models.Image{})
//		if result.Error != nil {
//			return fmt.Errorf("failed to batch delete images by identifiers in transaction: %w", result.Error)
//		}
//
//		return nil
//	})
//
//	return err
//}

func DeleteImagesByIdentifiersAndUser(identifiers []string, userID uint) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	var affectedCount int64
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("identifier IN ? AND user_id = ?", identifiers, userID).Delete(&models.Image{})
		if result.Error != nil {
			return fmt.Errorf("failed to batch delete images by identifiers and user ID in transaction: %w", result.Error)
		}

		affectedCount = result.RowsAffected
		return nil
	})

	if err != nil {
		return 0, err
	}

	return affectedCount, nil
}

func DeleteImageByIdentifierAndUser(identifier string, userID uint) error {
	if identifier == "" {
		return gorm.ErrRecordNotFound
	}

	err := dbcore.Transaction(func(tx *gorm.DB) error {

		result := tx.Where("identifier = ? AND user_id = ?", identifier, userID).Delete(&models.Image{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete image by identifier and user ID in transaction: %w", result.Error)
		}

		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
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
		searchPattern := fmt.Sprintf("%%%s%%", search)
		query = query.Where(
			"LOWER(original_name) LIKE LOWER(?) OR LOWER(identifier) LIKE LOWER(?) OR LOWER(file_hash) LIKE LOWER(?)",
			searchPattern, searchPattern, searchPattern,
		)
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

// GetSoftDeletedImageByHash 通过文件哈希查找一个被软删除的图片
func GetSoftDeletedImageByHash(hash string) (*models.Image, error) {
	var image models.Image
	err := dbcore.GetDBInstance().Unscoped().Where("file_hash = ? AND deleted_at IS NOT NULL", hash).First(&image).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// UpdateImageByIdentifier 通过Identifier更新图片信息
func UpdateImageByIdentifier(identifier string, updates map[string]interface{}) (*models.Image, error) {
	var image models.Image

	db := dbcore.GetDBInstance().Unscoped().Model(&models.Image{}).Where("identifier = ?", identifier)

	// 执行更新
	if err := db.Updates(updates).Error; err != nil {
		return nil, err
	}

	if err := db.First(&image).Error; err != nil {
		return nil, err
	}

	return &image, nil
}
