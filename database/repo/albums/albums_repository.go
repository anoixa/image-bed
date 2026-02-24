package albums

import (
	"context"
	"errors"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 相册仓库 - 封装所有相册相关的数据库操作
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的相册仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// AlbumInfo 包含图片数量和封面的相册信息
type AlbumInfo struct {
	Album      *models.Album
	ImageCount int64
	CoverURL   string
}

// GetUserAlbums 获取用户相册列表
func (r *Repository) GetUserAlbums(userID uint, page, pageSize int) ([]*AlbumInfo, int64, error) {
	var albums []*models.Album
	var total int64

	db := r.db.Model(&models.Album{}).Where("user_id = ?", userID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []*AlbumInfo{}, 0, nil
	}

	offset := (page - 1) * pageSize
	if err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&albums).Error; err != nil {
		return nil, 0, err
	}

	if len(albums) == 0 {
		return []*AlbumInfo{}, total, nil
	}

	albumIDs := make([]uint, len(albums))
	for i, album := range albums {
		albumIDs[i] = album.ID
	}

	var imageCounts []struct {
		AlbumID uint
		Count   int64
	}
	if err := r.db.Table("album_images").
		Select("album_id, COUNT(*) as count").
		Where("album_id IN ?", albumIDs).
		Group("album_id").
		Scan(&imageCounts).Error; err != nil {
		return nil, 0, err
	}

	countMap := make(map[uint]int64, len(imageCounts))
	for _, c := range imageCounts {
		countMap[c.AlbumID] = c.Count
	}

	var covers []struct {
		AlbumID    uint
		Identifier string
	}

	subQuery := r.db.Table("album_images ai").
		Select("ai.album_id, i.identifier, ROW_NUMBER() OVER (PARTITION BY ai.album_id ORDER BY i.created_at DESC) as rn").
		Joins("JOIN images i ON ai.image_id = i.id").
		Where("ai.album_id IN ?", albumIDs)

	if err := r.db.Table("(?) as ranked", subQuery).
		Where("rn = 1").
		Scan(&covers).Error; err != nil {
		return nil, 0, err
	}

	coverMap := make(map[uint]string, len(covers))
	for _, c := range covers {
		coverMap[c.AlbumID] = c.Identifier
	}

	result := make([]*AlbumInfo, len(albums))
	for i, album := range albums {
		result[i] = &AlbumInfo{
			Album:      album,
			ImageCount: countMap[album.ID],
			CoverURL:   coverMap[album.ID],
		}
	}

	return result, total, nil
}

// GetAlbumWithImagesByID 获取相册及其图片
func (r *Repository) GetAlbumWithImagesByID(albumID, userID uint) (*models.Album, error) {
	var album models.Album
	err := r.db.Preload("Images").First(&album, "id = ? AND user_id = ?", albumID, userID).Error
	return &album, err
}

// AddImageToAlbum 添加图片到相册
func (r *Repository) AddImageToAlbum(albumID, userID uint, image *models.Image) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("album not found or access denied")
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
				return errors.New("album not found or access denied")
			}
			return err
		}
		return tx.Model(&album).Association("Images").Delete(image)
	})
}

// AddImagesToAlbum 批量添加图片到相册
func (r *Repository) AddImagesToAlbum(albumID, userID uint, imageIDs []uint) error {
	if len(imageIDs) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("album not found or access denied")
			}
			return err
		}

		// 批量插入关联记录
		associations := make([]map[string]interface{}, len(imageIDs))
		for i, id := range imageIDs {
			associations[i] = map[string]interface{}{
				"album_id": albumID,
				"image_id": id,
			}
		}
		return tx.Table("album_images").Create(associations).Error
	})
}

// CreateAlbum 创建相册
func (r *Repository) CreateAlbum(album *models.Album) error {
	return r.db.Create(album).Error
}

// DeleteAlbum 删除相册
func (r *Repository) DeleteAlbum(albumID, userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("album not found or access denied")
			}
			return err
		}

		if err := tx.Model(&album).Association("Images").Clear(); err != nil {
			return err
		}
		return tx.Delete(&album).Error
	})
}

// GetAlbumByID 通过ID获取相册
func (r *Repository) GetAlbumByID(albumID uint) (*models.Album, error) {
	var album models.Album
	err := r.db.First(&album, albumID).Error
	return &album, err
}

// AlbumExists 检查相册是否存在
func (r *Repository) AlbumExists(albumID uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.Album{}).Where("id = ?", albumID).Count(&count).Error
	return count > 0, err
}

// UpdateAlbum 更新相册
func (r *Repository) UpdateAlbum(album *models.Album) error {
	return r.db.Save(album).Error
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}
