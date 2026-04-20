package images

import (
	"errors"
	"fmt"
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
	if newStatus == models.VariantStatusProcessing {
		updates["next_retry_at"] = nil
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
		"retry_count":   0,
		"next_retry_at": nil,
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

// UpdateFailed 更新失败状态（仅处理 processing 状态的变体）
func (r *VariantRepository) UpdateFailed(id uint, errMsg string) error {
	result := r.db.Model(&models.ImageVariant{}).
		Where("id = ? AND status = ?", id, models.VariantStatusProcessing).
		Updates(map[string]any{
			"status":        models.VariantStatusFailed,
			"error_message": errMsg,
			"next_retry_at": nil,
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("variant %d not in processing state", id)
	}
	return nil
}

// ForceUpdateFailed 无条件更新失败状态（用于提交前失败等变体尚不在 processing 状态的场景）
func (r *VariantRepository) ForceUpdateFailed(id uint, errMsg string) error {
	return r.db.Model(&models.ImageVariant{}).Where("id = ?", id).Updates(map[string]any{
		"status":        models.VariantStatusFailed,
		"error_message": errMsg,
		"next_retry_at": nil,
		"updated_at":    time.Now(),
	}).Error
}

// TouchProcessing refreshes updated_at for processing variants so the sweeper
// does not mistake long-running tasks for stale work.
func (r *VariantRepository) TouchProcessing(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}

	return r.db.Model(&models.ImageVariant{}).
		Where("id IN ? AND status = ?", ids, models.VariantStatusProcessing).
		Update("updated_at", time.Now()).
		Error
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

// ResetStaleProcessing resets processing variants older than the given
// duration back to pending so they can be retried. Returns the number of
// affected rows.
func (r *VariantRepository) ResetStaleProcessing(olderThan time.Duration) (int64, error) {
	reset, _, _, err := r.RecoverStaleProcessing(olderThan, 3)
	return reset, err
}

func (r *VariantRepository) RecoverStaleProcessing(olderThan time.Duration, maxRetries int) (resetCount, failedCount int64, retriedImageIDs []uint, err error) {
	cutoff := time.Now().Add(-olderThan)
	var staleVariants []models.ImageVariant
	if err := r.db.Where("status = ? AND updated_at < ?", models.VariantStatusProcessing, cutoff).Find(&staleVariants).Error; err != nil {
		return 0, 0, nil, err
	}

	now := time.Now()
	retriedSet := make(map[uint]bool)
	for _, variant := range staleVariants {
		nextRetryCount := variant.RetryCount + 1
		if maxRetries > 0 && nextRetryCount >= maxRetries {
			result := r.db.Model(&models.ImageVariant{}).
				Where("id = ? AND status = ?", variant.ID, models.VariantStatusProcessing).
				Updates(map[string]any{
					"status":        models.VariantStatusFailed,
					"error_message": fmt.Sprintf("stale processing exceeded retry limit (%d/%d)", nextRetryCount, maxRetries),
					"retry_count":   nextRetryCount,
					"next_retry_at": nil,
					"updated_at":    now,
				})
			if result.Error != nil {
				return resetCount, failedCount, retriedImageIDs, result.Error
			}
			failedCount += result.RowsAffected
			continue
		}

		nextRetryAt := now.Add(staleRetryDelay(nextRetryCount))
		result := r.db.Model(&models.ImageVariant{}).
			Where("id = ? AND status = ?", variant.ID, models.VariantStatusProcessing).
			Updates(map[string]any{
				"status":        models.VariantStatusPending,
				"error_message": "",
				"retry_count":   nextRetryCount,
				"next_retry_at": nextRetryAt,
				"updated_at":    now,
			})
		if result.Error != nil {
			return resetCount, failedCount, retriedImageIDs, result.Error
		}
		resetCount += result.RowsAffected
		retriedSet[variant.ImageID] = true
	}

	for id := range retriedSet {
		retriedImageIDs = append(retriedImageIDs, id)
	}
	return resetCount, failedCount, retriedImageIDs, nil
}

func staleRetryDelay(retryCount int) time.Duration {
	switch retryCount {
	case 1:
		return 5 * time.Minute
	case 2:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func (r *VariantRepository) ResetVariantsToPending(ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	result := r.db.Model(&models.ImageVariant{}).
		Where("id IN ? AND status = ?", ids, models.VariantStatusProcessing).
		Updates(map[string]any{
			"status":        models.VariantStatusPending,
			"error_message": "",
			"next_retry_at": nil,
			"updated_at":    time.Now(),
		})

	return result.RowsAffected, result.Error
}
