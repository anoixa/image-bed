package images

import (
	"context"
	"fmt"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Repository 图片仓库 - 封装所有图片相关的数据库操作
type Repository struct {
	db database.Provider
}

// NewRepository 创建新的图片仓库
func NewRepository(db database.Provider) *Repository {
	return &Repository{db: db}
}

// SaveImage 保存图片
func (r *Repository) SaveImage(image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Create(&image).Error
		if err != nil {
			return fmt.Errorf("failed to create image in transaction: %w", err)
		}
		return nil
	})
}

// CreateWithTx 在指定事务中创建图片记录
func (r *Repository) CreateWithTx(tx *gorm.DB, image *models.Image) error {
	return tx.Create(image).Error
}

// GetImageByHash 通过哈希获取图片
func (r *Repository) GetImageByHash(hash string) (*models.Image, error) {
	var image models.Image
	err := r.db.DB().Where("file_hash = ?", hash).First(&image).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// GetImageByIdentifier 通过标识符获取图片
func (r *Repository) GetImageByIdentifier(identifier string) (*models.Image, error) {
	var image models.Image
	result := r.db.DB().Where("identifier = ?", identifier).First(&image)
	return &image, result.Error
}

// DeleteImage 删除图片
func (r *Repository) DeleteImage(image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Delete(&image).Error
		if err != nil {
			return fmt.Errorf("failed to delete image in transaction: %w", err)
		}
		return err
	})
}

// DeleteImagesByIdentifiersAndUser 根据标识符和用户ID批量删除图片
func (r *Repository) DeleteImagesByIdentifiersAndUser(identifiers []string, userID uint) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}

	var affectedCount int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
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

// DeleteImageByIdentifierAndUser 根据标识符和用户ID删除图片
func (r *Repository) DeleteImageByIdentifierAndUser(identifier string, userID uint) error {
	if identifier == "" {
		return gorm.ErrRecordNotFound
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("identifier = ? AND user_id = ?", identifier, userID).Delete(&models.Image{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete image by identifier and user ID in transaction: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// ListImagesByUser 获取用户图片列表
func (r *Repository) ListImagesByUser(userID uint, page, pageSize int) ([]*models.Image, int64, error) {
	var images []*models.Image
	var total int64

	db := r.db.DB().Model(&models.Image{}).Where("user_id = ?", userID)

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
	err := r.db.DB().First(&image, id).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// GetImageByIDAndUser 通过ID和用户ID获取图片
func (r *Repository) GetImageByIDAndUser(id, userID uint) (*models.Image, error) {
	var image models.Image
	err := r.db.DB().Where("id = ? AND user_id = ?", id, userID).First(&image).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// UpdateImage 更新图片
func (r *Repository) UpdateImage(image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Save(image).Error
	})
}

// ImageExists 检查图片是否存在
func (r *Repository) ImageExists(identifier string) (bool, error) {
	var count int64
	err := r.db.DB().Model(&models.Image{}).Where("identifier = ?", identifier).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountImagesByUser 统计用户图片数量
func (r *Repository) CountImagesByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.DB().Model(&models.Image{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetSoftDeletedImageByHash 通过哈希获取软删除的图片
func (r *Repository) GetSoftDeletedImageByHash(hash string) (*models.Image, error) {
	var image models.Image
	err := r.db.DB().Unscoped().Where("file_hash = ? AND deleted_at IS NOT NULL", hash).First(&image).Error
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// UpdateImageByIdentifier 通过标识符更新图片
func (r *Repository) UpdateImageByIdentifier(identifier string, updates map[string]interface{}) (*models.Image, error) {
	var image models.Image
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Image{}).Where("identifier = ?", identifier).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update image by identifier in transaction: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("identifier = ?", identifier).First(&image).Error
	})
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// GetImageList 获取图片列表（支持搜索和过滤）
func (r *Repository) GetImageList(storageType, identifier, search string, page, pageSize, userID int) ([]*models.Image, int64, error) {
	var imageList []*models.Image
	var total int64

	db := r.db.DB().Model(&models.Image{}).Where("user_id = ?", userID)

	// 应用存储类型过滤
	if storageType != "" {
		db = db.Where("storage_driver = ?", storageType)
	}

	// 应用标识符过滤
	if identifier != "" {
		db = db.Where("identifier = ?", identifier)
	}

	// 应用搜索条件
	if search != "" {
		db = db.Where("original_name LIKE ?", "%"+search+"%")
	}

	// 获取总数
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&imageList).Error
	return imageList, total, err
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: &contextProvider{Provider: r.db, ctx: ctx}}
}

// DB 返回底层 *gorm.DB 实例
func (r *Repository) DB() *gorm.DB {
	return r.db.DB()
}

// contextProvider 包装 Provider 添加上下文
type contextProvider struct {
	database.Provider
	ctx context.Context
}

func (c *contextProvider) DB() *gorm.DB {
	return c.Provider.WithContext(c.ctx)
}

func (c *contextProvider) Transaction(fn database.TxFunc) error {
	return c.Provider.TransactionWithContext(c.ctx, fn)
}
