package albums

import (
	"context"
	"errors"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrAlbumNotFound 相册未找到或无权限
var ErrAlbumNotFound = errors.New("album not found or access denied")

// Repository 相册仓库
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

	// Single query: image count + cover per album using window functions
	var coverCounts []struct {
		AlbumID uint
		Count   int64
		Cover   string
	}
	subQuery := r.db.Table("album_images ai").
		Select("ai.album_id, COUNT(*) OVER (PARTITION BY ai.album_id) AS count, i.identifier AS cover, ROW_NUMBER() OVER (PARTITION BY ai.album_id ORDER BY i.created_at DESC) AS rn").
		Joins("JOIN images i ON ai.image_id = i.id").
		Where("ai.album_id IN ?", albumIDs)
	if err := r.db.Table("(?) AS sub", subQuery).
		Select("album_id, count, cover").
		Where("rn = 1").
		Scan(&coverCounts).Error; err != nil {
		return nil, 0, err
	}

	countMap := make(map[uint]int64, len(coverCounts))
	coverMap := make(map[uint]string, len(coverCounts))
	for _, c := range coverCounts {
		countMap[c.AlbumID] = c.Count
		coverMap[c.AlbumID] = c.Cover
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
				return ErrAlbumNotFound
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
				return ErrAlbumNotFound
			}
			return err
		}
		return tx.Model(&album).Association("Images").Delete(image)
	})
}

// AddImagesToAlbum 批量添加图片到相册
func (r *Repository) AddImagesToAlbum(albumID, userID uint, imageIDs []uint) (int64, error) {
	if len(imageIDs) == 0 {
		return 0, nil
	}
	var insertedCount int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlbumNotFound
			}
			return err
		}

		uniqueIDs := make([]uint, 0, len(imageIDs))
		seen := make(map[uint]struct{}, len(imageIDs))
		for _, id := range imageIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}

		var existingIDs []uint
		if err := tx.Table("album_images").
			Where("album_id = ? AND image_id IN ?", albumID, uniqueIDs).
			Pluck("image_id", &existingIDs).Error; err != nil {
			return err
		}

		existing := make(map[uint]struct{}, len(existingIDs))
		for _, id := range existingIDs {
			existing[id] = struct{}{}
		}

		associations := make([]map[string]any, 0, len(uniqueIDs))
		for _, id := range uniqueIDs {
			if _, ok := existing[id]; ok {
				continue
			}
			associations = append(associations, map[string]any{
				"album_id": albumID,
				"image_id": id,
			})
		}

		if len(associations) == 0 {
			return nil
		}

		if err := tx.Table("album_images").Create(associations).Error; err != nil {
			return err
		}
		insertedCount = int64(len(associations))
		return nil
	})
	return insertedCount, err
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
				return ErrAlbumNotFound
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

// UpdateAlbum 更新相册指定字段
func (r *Repository) UpdateAlbum(albumID uint, updates map[string]any) error {
	result := r.db.Model(&models.Album{}).Where("id = ?", albumID).Updates(updates)
	return result.Error
}

// RemoveImagesFromAlbum 批量从相册移除图片
func (r *Repository) RemoveImagesFromAlbum(albumID, userID uint, imageIDs []uint) (int64, error) {
	if len(imageIDs) == 0 {
		return 0, nil
	}

	var result int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var album models.Album
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&album, "id = ? AND user_id = ?", albumID, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlbumNotFound
			}
			return err
		}

		// 批量删除关联记录
		res := tx.Table("album_images").
			Where("album_id = ? AND image_id IN ?", albumID, imageIDs).
			Delete(nil)
		if res.Error != nil {
			return res.Error
		}

		result = res.RowsAffected
		return nil
	})

	return result, err
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}

// CountAlbumsByUser 统计用户的相册数量
func (r *Repository) CountAlbumsByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.Album{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}
