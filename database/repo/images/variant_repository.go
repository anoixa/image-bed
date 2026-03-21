package images

import (
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VariantRepository 图片变体仓库
type VariantRepository struct {
	db *gorm.DB
}

// NewVariantRepository 创建仓库
func NewVariantRepository(db *gorm.DB) *VariantRepository {
	return &VariantRepository{db: db}
}

// GetVariantsByImageID 获取图片的所有变体
func (r *VariantRepository) GetVariantsByImageID(imageID uint) ([]models.ImageVariant, error) {
	var variants []models.ImageVariant
	err := r.db.Where("image_id = ?", imageID).Find(&variants).Error
	return variants, err
}

// GetVariant 获取指定格式的变体
func (r *VariantRepository) GetVariant(imageID uint, format string) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	return &variant, err
}

// GetVariantByImageIDAndFormat 获取指定图片和格式的变体
func (r *VariantRepository) GetVariantByImageIDAndFormat(imageID uint, format string) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	return &variant, err
}

// GetByID 根据 ID 获取变体
func (r *VariantRepository) GetByID(id uint) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.First(&variant, id).Error
	return &variant, err
}

// UpsertPending 创建或获取 pending 状态的变体记录
func (r *VariantRepository) UpsertPending(imageID uint, format string) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error

	if err == nil {
		return &variant, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	variant = models.ImageVariant{
		ImageID: imageID,
		Format:  format,
		Status:  models.VariantStatusPending,
	}

	err = r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "image_id"}, {Name: "format"}},
		DoNothing: true,
	}).Create(&variant).Error
	if err != nil {
		return nil, err
	}

	err = r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	return &variant, err
}

// UpdateStatusCAS 条件更新状态
func (r *VariantRepository) UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error) {
	updates := map[string]any{
		"status":     newStatus,
		"updated_at": time.Now(),
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}

	result := r.db.Model(&models.ImageVariant{}).Where("id = ? AND status = ?", id, expected).Updates(updates)
	return result.RowsAffected > 0, result.Error
}

// UpdateCompleted 更新完成状态
func (r *VariantRepository) UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, fileHash string, width, height int) error {
	result := r.db.Model(&models.ImageVariant{}).Where("id = ? AND status = ?", id, models.VariantStatusProcessing).Updates(map[string]any{
		"status":        models.VariantStatusCompleted,
		"identifier":    identifier,
		"storage_path":  storagePath,
		"file_size":     fileSize,
		"file_hash":     fileHash,
		"width":         width,
		"height":        height,
		"error_message": "",
		"updated_at":    time.Now(),
	})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("CAS failed: variant not in processing state")
	}
	return nil
}

// UpdateFailed 更新失败状态
func (r *VariantRepository) UpdateFailed(id uint, errMsg string, _ bool) error {
	return r.db.Model(&models.ImageVariant{}).Where("id = ?", id).Updates(map[string]any{
		"status":        models.VariantStatusFailed,
		"error_message": errMsg,
		"updated_at":    time.Now(),
	}).Error
}

// GetImageByID 获取图片信息
func (r *VariantRepository) GetImageByID(imageID uint) (*models.Image, error) {
	var image models.Image
	err := r.db.First(&image, imageID).Error
	return &image, err
}

// DeleteByImageID 根据图片ID删除所有变体
func (r *VariantRepository) DeleteByImageID(imageID uint) error {
	return r.db.Where("image_id = ?", imageID).Delete(&models.ImageVariant{}).Error
}

// DeleteVariant 根据ID删除单个变体
func (r *VariantRepository) DeleteVariant(id uint) error {
	return r.db.Delete(&models.ImageVariant{}, id).Error
}

// GetMissingThumbnailVariants 批量查询需要生成缩略图的变体
func (r *VariantRepository) GetMissingThumbnailVariants(imageIDs []uint, formats []string) (map[uint]map[string]bool, error) {
	result := make(map[uint]map[string]bool, len(imageIDs))
	for _, imageID := range imageIDs {
		result[imageID] = make(map[string]bool, len(formats))
		for _, format := range formats {
			result[imageID][format] = false
		}
	}

	if len(imageIDs) == 0 || len(formats) == 0 {
		return result, nil
	}

	var variants []models.ImageVariant
	err := r.db.Select("image_id, format, status").Where("image_id IN ? AND format IN ?", imageIDs, formats).Find(&variants).Error
	if err != nil {
		return nil, err
	}

	for _, v := range variants {
		if v.Status == models.VariantStatusCompleted {
			if _, ok := result[v.ImageID]; ok {
				result[v.ImageID][v.Format] = true
			}
		}
	}

	return result, nil
}
