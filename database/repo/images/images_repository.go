package images

import (
	"context"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Repository 图片仓库
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的图片仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// SaveImage 保存图片
func (r *Repository) SaveImage(image *models.Image) error {
	return r.db.Create(&image).Error
}

// CreateWithTx 在指定事务中创建图片记录
func (r *Repository) CreateWithTx(tx *gorm.DB, image *models.Image) error {
	return tx.Create(image).Error
}

// GetImageByHash 通过哈希获取图片
func (r *Repository) GetImageByHash(hash string) (*models.Image, error) {
	var image models.Image
	err := r.db.Where("file_hash = ?", hash).First(&image).Error
	return &image, err
}

// GetImageByIdentifier 通过标识符获取图片
func (r *Repository) GetImageByIdentifier(identifier string) (*models.Image, error) {
	var image models.Image
	result := r.db.Where("identifier = ?", identifier).First(&image)
	return &image, result.Error
}

// DeleteImage 删除图片
func (r *Repository) DeleteImage(image *models.Image) error {
	return r.db.Delete(&image).Error
}

// DeleteImagesByIdentifiersAndUser 根据标识符和用户ID批量删除图片
func (r *Repository) DeleteImagesByIdentifiersAndUser(identifiers []string, userID uint) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	result := r.db.Where("identifier IN ? AND user_id = ?", identifiers, userID).Delete(&models.Image{})
	return result.RowsAffected, result.Error
}

// GetImagesByIdentifiersAndUser 批量查询用户的图片（使用 IN 语句，避免 N+1 查询）
func (r *Repository) GetImagesByIdentifiersAndUser(identifiers []string, userID uint) ([]*models.Image, error) {
	if len(identifiers) == 0 {
		return []*models.Image{}, nil
	}

	var images []*models.Image
	err := r.db.Where("identifier IN ? AND user_id = ?", identifiers, userID).Find(&images).Error
	return images, err
}

// DeleteImageByIdentifierAndUser 根据标识符和用户ID删除图片
func (r *Repository) DeleteImageByIdentifierAndUser(identifier string, userID uint) error {
	if identifier == "" {
		return gorm.ErrRecordNotFound
	}

	result := r.db.Where("identifier = ? AND user_id = ?", identifier, userID).Delete(&models.Image{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// ListImagesByUser 获取用户图片列表
func (r *Repository) ListImagesByUser(userID uint, page, pageSize int) ([]*models.Image, int64, error) {
	var images []*models.Image
	var total int64

	db := r.db.Model(&models.Image{}).Where("user_id = ?", userID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&images).Error
	return images, total, err
}

// GetImageByID 通过ID获取图片
func (r *Repository) GetImageByID(id uint) (*models.Image, error) {
	var image models.Image
	err := r.db.First(&image, id).Error
	return &image, err
}

// GetImageByIDAndUser 通过ID和用户ID获取图片
func (r *Repository) GetImageByIDAndUser(id, userID uint) (*models.Image, error) {
	var image models.Image
	err := r.db.Where("id = ? AND user_id = ?", id, userID).First(&image).Error
	return &image, err
}

// GetImagesByIDsAndUser 批量通过ID和用户ID获取图片
func (r *Repository) GetImagesByIDsAndUser(ids []uint, userID uint) ([]*models.Image, error) {
	if len(ids) == 0 {
		return []*models.Image{}, nil
	}
	var images []*models.Image
	err := r.db.Where("id IN ? AND user_id = ?", ids, userID).Find(&images).Error
	return images, err
}

// UpdateImage 更新图片
func (r *Repository) UpdateImage(image *models.Image) error {
	return r.db.Save(image).Error
}

// MarkAsPendingDeletion 将图片标记为待删除状态
func (r *Repository) MarkAsPendingDeletion(identifiers []string, userID uint) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	result := r.db.Model(&models.Image{}).
		Where("identifier IN ? AND user_id = ?", identifiers, userID).
		Update("is_pending_deletion", true)

	return result.RowsAffected, result.Error
}

// DeletePendingImages 删除已标记为待删除的图片
func (r *Repository) DeletePendingImages(identifiers []string, userID uint) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	result := r.db.Where("identifier IN ? AND user_id = ? AND is_pending_deletion = ?", identifiers, userID, true).Delete(&models.Image{})
	return result.RowsAffected, result.Error
}

// ImageExists 检查图片是否存在
func (r *Repository) ImageExists(identifier string) (bool, error) {
	var count int64
	err := r.db.Model(&models.Image{}).Where("identifier = ?", identifier).Count(&count).Error
	return count > 0, err
}

// CountImagesByUser 统计用户图片数量
func (r *Repository) CountImagesByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.Image{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetSoftDeletedImageByHash 通过哈希获取软删除的图片
func (r *Repository) GetSoftDeletedImageByHash(hash string) (*models.Image, error) {
	var image models.Image
	err := r.db.Unscoped().Where("file_hash = ? AND deleted_at IS NOT NULL", hash).First(&image).Error
	return &image, err
}

// UpdateImageByIdentifier 通过标识符更新图片
func (r *Repository) UpdateImageByIdentifier(identifier string, updates map[string]interface{}) (*models.Image, error) {
	result := r.db.Model(&models.Image{}).Where("identifier = ?", identifier).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var image models.Image
	err := r.db.Where("identifier = ?", identifier).First(&image).Error
	return &image, err
}

// GetImageList 获取图片列表
func (r *Repository) GetImageList(storageType, identifier, search string, albumID *uint, startTime, endTime int64, sort string, page, pageSize, userID int) ([]*models.Image, int64, error) {
	var imageList []*models.Image
	var total int64

	db := r.db.Model(&models.Image{}).Where("user_id = ?", userID)

	if storageType != "" {
		db = db.Where("storage_driver = ?", storageType)
	}
	if identifier != "" {
		db = db.Where("identifier = ?", identifier)
	}
	if search != "" {
		db = db.Where("original_name LIKE ?", "%"+search+"%")
	}
	if albumID != nil {
		db = db.Joins("JOIN album_images ON album_images.image_id = images.id").
			Where("album_images.album_id = ?", *albumID)
	}
	// 时间区间过滤（Unix时间戳秒）
	if startTime > 0 {
		db = db.Where("created_at >= ?", time.Unix(startTime, 0))
	}
	if endTime > 0 {
		db = db.Where("created_at <= ?", time.Unix(endTime, 0))
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize

	// 根据 sort 参数设置排序方向
	orderBy := "created_at desc"
	if sort == "asc" {
		orderBy = "created_at asc"
	}

	err := db.Order(orderBy).Offset(offset).Limit(pageSize).Find(&imageList).Error
	return imageList, total, err
}

// GetImagesByAlbumID 根据相册ID获取图片列表
func (r *Repository) GetImagesByAlbumID(albumID uint, page, pageSize int) ([]*models.Image, int64, error) {
	var imageList []*models.Image
	var total int64

	db := r.db.Model(&models.Image{}).
		Joins("JOIN album_images ON album_images.image_id = images.id").
		Where("album_images.album_id = ?", albumID)

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("images.created_at desc").Offset(offset).Limit(pageSize).Find(&imageList).Error
	return imageList, total, err
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}

// DB 返回底层 *gorm.DB 实例
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// UpdateVariantStatus 更新图片变体状态
func (r *Repository) UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error {
	return r.db.Model(&models.Image{}).Where("id = ?", imageID).Update("variant_status", status).Error
}

// GetImagesByVariantStatus 根据变体状态获取图片列表
func (r *Repository) GetImagesByVariantStatus(statuses []models.ImageVariantStatus, limit int) ([]*models.Image, error) {
	var images []*models.Image
	err := r.db.Where("variant_status IN ?", statuses).Limit(limit).Find(&images).Error
	return images, err
}
