package images

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo"
	"gorm.io/gorm"
)

// Repository 图片仓库
type Repository struct {
	db *gorm.DB
	*repo.GenericRepository[models.Image]
}

var imageListSelectColumns = []string{
	"images.id",
	"images.identifier",
	"images.original_name",
	"images.file_size",
	"images.mime_type",
	"images.width",
	"images.height",
	"images.is_public",
	"images.created_at",
}

// RandomImageFilter 随机图片筛选条件
type RandomImageFilter struct {
	AlbumID          *uint
	IncludeAllPublic bool
	MinWidth         int
	MinHeight        int
	MaxWidth         int
	MaxHeight        int
	RequireWebP      bool  // 是否要求必须有WebP变体
	MaxFileSize      int64 // 最大文件大小（字节）
}

// NewRepository 创建新的图片仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{
		db:                db,
		GenericRepository: repo.NewGenericRepository[models.Image](db),
	}
}

// SaveImage 保存图片
func (r *Repository) SaveImage(image *models.Image) error {
	return r.db.Create(&image).Error
}

// CreateWithTx 在指定事务中创建图片记录
func (r *Repository) CreateWithTx(tx *gorm.DB, image *models.Image) error {
	return tx.Create(image).Error
}

// CountImagesByStoragePath 统计使用相同存储路径的图片数量（用于秒传引用计数）
func (r *Repository) CountImagesByStoragePath(storagePath string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Image{}).Where("storage_path = ? AND deleted_at IS NULL", storagePath).Count(&count).Error
	return count, err
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

// GetImagesByIdentifiersAndUser 批量查询用户的图片
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

	return nil
}

// CountImagesByStorageConfig 统计指定存储配置下的图片数量
func (r *Repository) CountImagesByStorageConfig(storageConfigID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.Image{}).Where("storage_config_id = ?", storageConfigID).Count(&count).Error
	return count, err
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
func (r *Repository) UpdateImageByIdentifier(identifier string, updates map[string]any) (*models.Image, error) {
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
func (r *Repository) GetImageList(storageConfigIDs []uint, identifier, search string, albumID *uint, startTime, endTime int64, sort string, page, pageSize, userID int) ([]*models.Image, int64, error) {
	var imageList []*models.Image
	var total int64

	db := r.db.Model(&models.Image{}).Where("user_id = ?", userID)

	if len(storageConfigIDs) > 0 {
		db = db.Where("storage_config_id IN ?", storageConfigIDs)
	}
	if identifier != "" {
		db = db.Where("identifier = ?", identifier)
	}
	if search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(search)
		db = db.Where("original_name LIKE ? ESCAPE '\\'", "%"+escaped+"%")
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

	err := db.Select(imageListSelectColumns).Order(orderBy).Offset(offset).Limit(pageSize).Find(&imageList).Error
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

// MarkStaleProcessingAsFailed updates stale processing images to Failed if they
// have no processing variants but at least one failed variant.
// excludeIDs are excluded from the update (e.g. re-triggered images).
func (r *Repository) MarkStaleProcessingAsFailed(cutoff time.Time, excludeIDs []uint) (int64, error) {
	processingVariants := r.db.Table("image_variants").Select("1").
		Where("image_variants.image_id = images.id AND image_variants.status = ?", models.VariantStatusProcessing)
	failedVariants := r.db.Table("image_variants").Select("1").
		Where("image_variants.image_id = images.id AND image_variants.status = ?", models.VariantStatusFailed)

	q := r.db.Model(&models.Image{}).
		Where("variant_status = ? AND updated_at < ?", models.ImageVariantStatusProcessing, cutoff).
		Where("NOT EXISTS (?)", processingVariants).
		Where("EXISTS (?)", failedVariants)

	if len(excludeIDs) > 0 {
		q = q.Where("id NOT IN ?", excludeIDs)
	}

	result := q.Update("variant_status", models.ImageVariantStatusFailed)
	return result.RowsAffected, result.Error
}

// ResetStaleProcessingToNone resets stale processing images back to None when
// they have no remaining processing variants.
// excludeIDs are excluded from the update (e.g. re-triggered images).
func (r *Repository) ResetStaleProcessingToNone(cutoff time.Time, excludeIDs []uint) (int64, error) {
	processingVariants := r.db.Table("image_variants").Select("1").
		Where("image_variants.image_id = images.id AND image_variants.status = ?", models.VariantStatusProcessing)

	q := r.db.Model(&models.Image{}).
		Where("variant_status = ? AND updated_at < ?", models.ImageVariantStatusProcessing, cutoff).
		Where("NOT EXISTS (?)", processingVariants)

	if len(excludeIDs) > 0 {
		q = q.Where("id NOT IN ?", excludeIDs)
	}

	result := q.Update("variant_status", models.ImageVariantStatusNone)
	return result.RowsAffected, result.Error
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}

// DB
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// RemoveImageFromAllAlbums 从所有相册中移除图片关联
func (r *Repository) RemoveImageFromAllAlbums(imageID uint) error {
	return r.db.Table("album_images").Where("image_id = ?", imageID).Delete(nil).Error
}

// RemoveImagesFromAllAlbums 批量从所有相册中移除图片关联
func (r *Repository) RemoveImagesFromAllAlbums(imageIDs []uint) error {
	if len(imageIDs) == 0 {
		return nil
	}
	return r.db.Table("album_images").Where("image_id IN ?", imageIDs).Delete(nil).Error
}

// UpdateVariantStatus 更新图片变体状态
func (r *Repository) UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error {
	return r.db.Model(&models.Image{}).Where("id = ?", imageID).Update("variant_status", status).Error
}

func (r *Repository) ResetProcessingVariantStatus(imageIDs []uint, status models.ImageVariantStatus) (int64, error) {
	if len(imageIDs) == 0 {
		return 0, nil
	}

	result := r.db.Model(&models.Image{}).
		Where("id IN ? AND variant_status = ?", imageIDs, models.ImageVariantStatusProcessing).
		Updates(map[string]any{
			"variant_status": status,
			"updated_at":     time.Now(),
		})

	return result.RowsAffected, result.Error
}

// TouchVariantProcessingStatus refreshes updated_at while an image remains in
// processing so stale detection does not race with active work.
func (r *Repository) TouchVariantProcessingStatus(imageID uint) error {
	return r.db.Model(&models.Image{}).
		Where("id = ? AND variant_status = ?", imageID, models.ImageVariantStatusProcessing).
		Update("updated_at", time.Now()).
		Error
}

// GetImagesByVariantStatus 根据变体状态获取图片列表
func (r *Repository) GetImagesByVariantStatus(statuses []models.ImageVariantStatus, limit int) ([]*models.Image, error) {
	var images []*models.Image
	err := r.db.Where("variant_status IN ?", statuses).Limit(limit).Find(&images).Error
	return images, err
}

// GetRandomPublicImage 随机获取一张公开图片
func (r *Repository) GetRandomPublicImage(filter *RandomImageFilter) (*models.Image, error) {
	db := r.db.Model(&models.Image{}).Where("is_public = ?", true)

	if filter != nil && filter.AlbumID != nil && !filter.IncludeAllPublic {
		db = db.Joins("JOIN album_images ON album_images.image_id = images.id").
			Where("album_images.album_id = ?", *filter.AlbumID)
	}

	// 尺寸筛选
	if filter != nil {
		if filter.MinWidth > 0 {
			db = db.Where("width >= ?", filter.MinWidth)
		}
		if filter.MinHeight > 0 {
			db = db.Where("height >= ?", filter.MinHeight)
		}
		if filter.MaxWidth > 0 {
			db = db.Where("width <= ?", filter.MaxWidth)
		}
		if filter.MaxHeight > 0 {
			db = db.Where("height <= ?", filter.MaxHeight)
		}
		// 文件大小限制
		if filter.MaxFileSize > 0 {
			db = db.Where("file_size <= ?", filter.MaxFileSize)
		}
		if filter.RequireWebP {
			db = db.Where("variant_status = ?", models.ImageVariantStatusCompleted)
		}
	}

	idQuery := db.Session(&gorm.Session{}).
		Distinct("images.id").
		Select("images.id")

	var bounds struct {
		MinID uint
		MaxID uint
	}
	if err := r.db.Table("(?) AS filtered_images", idQuery).
		Select("MIN(id) AS min_id, MAX(id) AS max_id").
		Scan(&bounds).Error; err != nil {
		return nil, err
	}
	if bounds.MaxID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	targetID, err := randomUint(bounds.MinID, bounds.MaxID)
	if err != nil {
		return nil, err
	}

	var selected struct {
		ID uint
	}
	selectByTarget := func(operator, order string) error {
		return db.Session(&gorm.Session{}).
			Select("images.id").
			Distinct("images.id").
			Where("images.id "+operator+" ?", targetID).
			Order(order).
			Limit(1).
			Take(&selected).Error
	}

	if err := selectByTarget(">=", "images.id ASC"); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err := selectByTarget("<", "images.id DESC"); err != nil {
			return nil, err
		}
	}

	var image models.Image
	if err := r.db.First(&image, selected.ID).Error; err != nil {
		return nil, err
	}
	return &image, nil
}

func randomUint(minID, maxID uint) (uint, error) {
	if maxID <= minID {
		return minID, nil
	}

	width := int64(maxID-minID) + 1
	n, err := rand.Int(rand.Reader, big.NewInt(width))
	if err != nil {
		return 0, err
	}
	return minID + uint(n.Int64()), nil
}

// DeleteBatchResult 批量删除结果
type DeleteBatchResult struct {
	DeletedCount int64
	ImageIDs     []uint
}

// DeleteBatchTransaction 在事务中批量删除图片及其关联数据
func (r *Repository) DeleteBatchTransaction(ctx context.Context, identifiers []string, userID uint) (*DeleteBatchResult, []*models.Image, error) {
	if len(identifiers) == 0 {
		return &DeleteBatchResult{DeletedCount: 0, ImageIDs: []uint{}}, []*models.Image{}, nil
	}

	var result DeleteBatchResult
	var imagesToDelete []*models.Image

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 查询要删除的图片
		if err := tx.Where("identifier IN ? AND user_id = ?", identifiers, userID).Find(&imagesToDelete).Error; err != nil {
			return fmt.Errorf("failed to get images: %w", err)
		}

		if len(imagesToDelete) == 0 {
			return nil
		}

		// 收集图片ID
		imageIDs := make([]uint, len(imagesToDelete))
		for i, img := range imagesToDelete {
			imageIDs[i] = img.ID
		}

		// 2. 从所有相册中移除关联
		if err := tx.Table("album_images").Where("image_id IN ?", imageIDs).Delete(nil).Error; err != nil {
			return fmt.Errorf("failed to remove images from albums: %w", err)
		}

		// 3. 删除图片记录
		deleteResult := tx.Where("identifier IN ? AND user_id = ?", identifiers, userID).Delete(&models.Image{})
		if deleteResult.Error != nil {
			return fmt.Errorf("failed to delete images: %w", deleteResult.Error)
		}

		result.DeletedCount = deleteResult.RowsAffected
		result.ImageIDs = imageIDs
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return &result, imagesToDelete, nil
}
