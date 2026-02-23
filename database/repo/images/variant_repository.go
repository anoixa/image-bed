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
	updates := map[string]interface{}{
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
func (r *VariantRepository) UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, width, height int) error {
	result := r.db.Model(&models.ImageVariant{}).Where("id = ? AND status = ?", id, models.VariantStatusProcessing).Updates(map[string]interface{}{
		"status":        models.VariantStatusCompleted,
		"identifier":    identifier,
		"storage_path":  storagePath,
		"file_size":     fileSize,
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
func (r *VariantRepository) UpdateFailed(id uint, errMsg string, allowRetry bool) error {
	updates := map[string]interface{}{
		"status":        models.VariantStatusFailed,
		"error_message": errMsg,
		"updated_at":    time.Now(),
	}
	if allowRetry {
		updates["retry_count"] = gorm.Expr("retry_count + 1")
		updates["next_retry_at"] = time.Now().Add(5 * time.Minute)
	}
	return r.db.Model(&models.ImageVariant{}).Where("id = ?", id).Updates(updates).Error
}

// calculateBackoff 计算指数退避时间
func calculateBackoff(base time.Duration, retryCount int) time.Duration {
	if retryCount >= 5 {
		return 60 * time.Minute
	}
	return base * time.Duration(1<<retryCount)
}

// ResetForRetry CAS：只有 failed 才能转为 pending
func (r *VariantRepository) ResetForRetry(id uint, baseBackoff time.Duration) error {
	var variant models.ImageVariant
	if err := r.db.First(&variant, id).Error; err != nil {
		return err
	}

	nextRetry := time.Now().Add(calculateBackoff(baseBackoff, variant.RetryCount))

	result := r.db.Model(&models.ImageVariant{}).Where("id = ? AND status = ?", id, models.VariantStatusFailed).Updates(map[string]interface{}{
		"retry_count":   gorm.Expr("retry_count + 1"),
		"next_retry_at": nextRetry,
		"status":        models.VariantStatusPending,
		"updated_at":    time.Now(),
	})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("variant is not in failed state")
	}
	return nil
}

// GetRetryableVariants 查询可重试的变体
func (r *VariantRepository) GetRetryableVariants(now time.Time, limit int) ([]models.ImageVariant, error) {
	var variants []models.ImageVariant
	err := r.db.Where("status = ? AND retry_count < ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
		models.VariantStatusFailed, 3, now).Limit(limit).Find(&variants).Error
	return variants, err
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

// GetOrphanVariants 获取长时间处于 processing 状态的孤儿任务
func (r *VariantRepository) GetOrphanVariants(threshold time.Duration, limit int) ([]models.ImageVariant, error) {
	cutoff := time.Now().Add(-threshold)
	var variants []models.ImageVariant
	err := r.db.Where("status = ? AND updated_at < ?", models.VariantStatusProcessing, cutoff).Limit(limit).Find(&variants).Error
	return variants, err
}

// ResetProcessingToPending 将 processing 状态重置为 pending
func (r *VariantRepository) ResetProcessingToPending(id uint) error {
	return r.db.Model(&models.ImageVariant{}).Where("id = ? AND status = ?", id, models.VariantStatusProcessing).Updates(map[string]interface{}{
		"status":     models.VariantStatusPending,
		"updated_at": time.Now(),
	}).Error
}
