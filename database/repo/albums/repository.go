package albums

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 相册仓库 - 封装所有相册相关的数据库操作
type Repository struct {
	db database.Provider
}

// NewRepository 创建新的相册仓库
func NewRepository(db database.Provider) *Repository {
	return &Repository{db: db}
}

// GetUserAlbums 获取用户相册列表
func (r *Repository) GetUserAlbums(userID uint, page, pageSize int) ([]*models.Album, int64, error) {
	var albums []*models.Album
	var total int64
	db := r.db.DB().Model(&models.Album{}).Where("user_id = ?", userID)

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&albums).Error
	return albums, total, err
}

// GetAlbumWithImagesByID 获取相册及其图片
func (r *Repository) GetAlbumWithImagesByID(albumID, userID uint) (*models.Album, error) {
	var album models.Album
	err := r.db.DB().Preload("Images").First(&album, "id = ? AND user_id = ?", albumID, userID).Error
	return &album, err
}

// AddImageToAlbum 添加图片到相册
func (r *Repository) AddImageToAlbum(albumID, userID uint, image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("album with ID %d not found or access denied", albumID)
			}
			return err
		}

		return tx.Model(&album).Association("Images").Append(image)
	})
}

// RemoveImageFromAlbum 从相册移除图片
func (r *Repository) RemoveImageFromAlbum(albumID, userID uint, image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("album with ID %d not found or access denied", albumID)
			}
			return err
		}

		return tx.Model(&album).Association("Images").Delete(image)
	})
}

// CreateAlbum 创建相册
func (r *Repository) CreateAlbum(album *models.Album) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&album).Error; err != nil {
			return fmt.Errorf("failed to create album in transaction: %w", err)
		}
		return nil
	})
}

// DeleteAlbum 删除相册
func (r *Repository) DeleteAlbum(albumID, userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("album with ID %d not found or access denied", albumID)
			}
			return err
		}

		if err := tx.Model(&album).Association("Images").Clear(); err != nil {
			return fmt.Errorf("failed to clear image associations for album %d: %w", albumID, err)
		}

		if err := tx.Delete(&album).Error; err != nil {
			return fmt.Errorf("failed to delete album %d: %w", albumID, err)
		}

		return nil
	})
}

// GetAlbumByID 通过ID获取相册
func (r *Repository) GetAlbumByID(albumID uint) (*models.Album, error) {
	var album models.Album
	err := r.db.DB().First(&album, albumID).Error
	if err != nil {
		return nil, err
	}
	return &album, nil
}

// AlbumExists 检查相册是否存在
func (r *Repository) AlbumExists(albumID uint) (bool, error) {
	var count int64
	err := r.db.DB().Model(&models.Album{}).Where("id = ?", albumID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateAlbum 更新相册
func (r *Repository) UpdateAlbum(album *models.Album) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Save(album).Error
	})
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: &contextProvider{Provider: r.db, ctx: ctx}}
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
