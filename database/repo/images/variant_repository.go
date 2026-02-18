package images

import (
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VariantRepository 图片变体仓库接口
type VariantRepository interface {
	GetVariantsByImageID(imageID uint) ([]models.ImageVariant, error)
	GetVariant(imageID uint, format string) (*models.ImageVariant, error)
	GetVariantByImageIDAndFormat(imageID uint, format string) (*models.ImageVariant, error)
	GetByID(id uint) (*models.ImageVariant, error)
	UpsertPending(imageID uint, format string) (*models.ImageVariant, error)
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier string, fileSize int64, width, height int) error
	UpdateFailed(id uint, errMsg string, allowRetry bool) error
	ResetForRetry(id uint, baseBackoff time.Duration) error
	GetRetryableVariants(now time.Time, limit int) ([]models.ImageVariant, error)
	GetImageByID(imageID uint) (*models.Image, error)
	DeleteByImageID(imageID uint) error
	GetMissingThumbnailVariants(imageIDs []uint, formats []string) (map[uint]map[string]bool, error)
}

// MissingVariantInfo 缺失变体信息
type MissingVariantInfo struct {
	ImageID uint
	Format  string
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

// GetVariantByImageIDAndFormat 获取指定图片和格式的变体
func (r *variantRepository) GetVariantByImageIDAndFormat(imageID uint, format string) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.Where("image_id = ? AND format = ?", imageID, format).First(&variant).Error
	if err != nil {
		return nil, err
	}
	return &variant, nil
}

// GetByID 根据 ID 获取变体
func (r *variantRepository) GetByID(id uint) (*models.ImageVariant, error) {
	var variant models.ImageVariant
	err := r.db.First(&variant, id).Error
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
			utils.LogIfDevf("[UpsertPending] Found existing variant %d with status=%s, retry_count=%d, image_id=%d",
				variant.ID, variant.Status, variant.RetryCount, imageID)
		}
		return &variant, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 创建新记录
	variant = models.ImageVariant{
		ImageID:    imageID,
		Format:     format,
		Status:     models.VariantStatusPending,
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

// UpdateFailed 更新失败状态
func (r *variantRepository) UpdateFailed(id uint, errMsg string, allowRetry bool) error {
	updates := map[string]interface{}{
		"status":        models.VariantStatusFailed,
		"error_message": errMsg,
		"updated_at":    time.Now(),
	}

	if allowRetry {
		// 允许重试，增加重试计数
		updates["retry_count"] = gorm.Expr("retry_count + 1")
	}

	return r.db.Model(&models.ImageVariant{}).Where("id = ?", id).Updates(updates).Error
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
		utils.LogIfDevf("[GetRetryableVariants] Found variant %d: status=%s, retry_count=%d, next_retry_at=%v",
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

// GetMissingThumbnailVariants 批量查询需要生成缩略图的变体
// 返回 map[imageID]map[format]exists，其中 exists=false 表示需要生成
func (r *variantRepository) GetMissingThumbnailVariants(imageIDs []uint, formats []string) (map[uint]map[string]bool, error) {
	// 初始化结果：假设所有图片都需要所有尺寸
	result := make(map[uint]map[string]bool, len(imageIDs))
	for _, imageID := range imageIDs {
		result[imageID] = make(map[string]bool, len(formats))
		for _, format := range formats {
			result[imageID][format] = false // 默认需要生成
		}
	}

	if len(imageIDs) == 0 || len(formats) == 0 {
		return result, nil
	}

	// 查询数据库中已存在的变体
	var variants []models.ImageVariant
	err := r.db.Select("image_id, format, status").
		Where("image_id IN ? AND format IN ?", imageIDs, formats).
		Find(&variants).Error
	if err != nil {
		return nil, err
	}

	// 标记已存在的变体
	for _, v := range variants {
		// 如果变体状态是 completed，则不需要重新生成
		if v.Status == models.VariantStatusCompleted {
			if _, ok := result[v.ImageID]; ok {
				result[v.ImageID][v.Format] = true
			}
		}
	}

	return result, nil
}
