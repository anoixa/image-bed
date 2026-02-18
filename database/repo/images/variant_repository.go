package images

import (
	"errors"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VariantRepository 图片变体仓库接口
type VariantRepository interface {
	GetVariantsByImageID(imageID uint) ([]models.ImageVariant, error)
	GetVariant(imageID uint, format string) (*models.ImageVariant, error)
	UpsertPending(imageID uint, format string) (*models.ImageVariant, error)
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier string, fileSize int64, width, height int) error
	ResetForRetry(id uint, baseBackoff time.Duration) error
	GetRetryableVariants(now time.Time, limit int) ([]models.ImageVariant, error)
	GetImageByID(imageID uint) (*models.Image, error)
	DeleteByImageID(imageID uint) error
}

// variantRepository 实现
type variantRepository struct {
	db *gorm.DB
}

// NewVariantRepository 创建仓库
func NewVariantRepository(db *gorm.DB) VariantRepository {
	return &variantRepository{db: db}
}

// GetVariantsByImageID 获取图片的所有变体
func (r *variantRepository) GetVariantsByImageID(imageID uint) ([]models.ImageVariant, error) {
	var variants []models.ImageVariant
	err := r.db.Where("image_id = ?", imageID).Find(&variants).Error
	return variants, err
}

// GetVariant 获取指定格式的变体
func (r *variantRepository) GetVariant(imageID uint, format string) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	if err != nil {
		return nil, err
	}
	return &variant, nil
}

// UpsertPending 创建或获取 pending 状态的变体记录
func (r *variantRepository) UpsertPending(imageID uint, format string) (*models.ImageVariant, error) {
	// 先尝试查找现有记录
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error

	if err == nil {
		// 记录已存在，记录状态信息用于调试
		if variant.Status != models.VariantStatusPending {
			log.Printf("[UpsertPending] Found existing variant %d with status=%s, retry_count=%d, image_id=%d",
				variant.ID, variant.Status, variant.RetryCount, imageID)
		}
		return &variant, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 创建新记录
	variant = models.ImageVariant{
		ImageID:  imageID,
		Format:   format,
		Status:   models.VariantStatusPending,
		Identifier: "",
	}

	// 使用 OnConflict 处理并发创建
	err = r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "image_id"}, {Name: "format"}},
		DoNothing: true,
	}).Create(&variant).Error

	if err != nil {
		return nil, err
	}

	// 再次查询获取记录（包括 ID）
	err = r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	if err != nil {
		return nil, err
	}

	return &variant, nil
}

// UpdateStatusCAS 条件更新状态（Compare-And-Swap）
func (r *variantRepository) UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error) {
	updates := map[string]interface{}{
		"status":     newStatus,
		"updated_at": time.Now(),
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}

	result := r.db.Model(&models.ImageVariant{}).
		Where("id = ? AND status = ?", id, expected).
		Updates(updates)

	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

// UpdateCompleted 原子更新完成状态（包含元数据）
func (r *variantRepository) UpdateCompleted(id uint, identifier string, fileSize int64, width, height int) error {
	result := r.db.Model(&models.ImageVariant{}).
		Where("id = ? AND status = ?", id, models.VariantStatusProcessing).
		Updates(map[string]interface{}{
			"status":        models.VariantStatusCompleted,
			"identifier":    identifier,
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

// calculateBackoff 计算指数退避时间
// retryCount: 0->5min, 1->10min, 2->20min, 3->40min, 4->60min(max)
func calculateBackoff(base time.Duration, retryCount int) time.Duration {
	if retryCount >= 5 {
		return 60 * time.Minute
	}
	return base * time.Duration(1<<retryCount)
}

// ResetForRetry CAS：只有 failed 才能转为 pending
func (r *variantRepository) ResetForRetry(id uint, baseBackoff time.Duration) error {
	var variant models.ImageVariant
	if err := r.db.First(&variant, id).Error; err != nil {
		return err
	}

	nextRetry := time.Now().Add(calculateBackoff(baseBackoff, variant.RetryCount))

	result := r.db.Model(&models.ImageVariant{}).
		Where("id = ? AND status = ?", id, models.VariantStatusFailed).
		Updates(map[string]interface{}{
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
func (r *variantRepository) GetRetryableVariants(now time.Time, limit int) ([]models.ImageVariant, error) {
	var variants []models.ImageVariant
	err := r.db.Where("status = ? AND retry_count < ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
		models.VariantStatusFailed,
		3, // maxRetries
		now,
	).Limit(limit).Find(&variants).Error
	if err != nil {
		return nil, err
	}
	for _, v := range variants {
		log.Printf("[GetRetryableVariants] Found variant %d: status=%s, retry_count=%d, next_retry_at=%v",
			v.ID, v.Status, v.RetryCount, v.NextRetryAt)
	}
	return variants, err
}

// GetImageByID 获取图片信息
func (r *variantRepository) GetImageByID(imageID uint) (*models.Image, error) {
	var image models.Image
	err := r.db.First(&image, imageID).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// DeleteByImageID 根据图片ID删除所有变体
func (r *variantRepository) DeleteByImageID(imageID uint) error {
	return r.db.Where("image_id = ?", imageID).Delete(&models.ImageVariant{}).Error
}
