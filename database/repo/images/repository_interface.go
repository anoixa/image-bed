package images

import (
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// RepositoryInterface 图片仓库接口
type RepositoryInterface interface {
	// SaveImage 保存图片
	SaveImage(image *models.Image) error
	// CreateWithTx 在指定事务中创建图片记录
	CreateWithTx(tx *gorm.DB, image *models.Image) error
	// GetImageByHash 通过哈希获取图片
	GetImageByHash(hash string) (*models.Image, error)
	// GetImageByIdentifier 通过标识符获取图片
	GetImageByIdentifier(identifier string) (*models.Image, error)
	// DeleteImage 删除图片
	DeleteImage(image *models.Image) error
	// DeleteImagesByIdentifiersAndUser 根据标识符和用户ID批量删除图片
	DeleteImagesByIdentifiersAndUser(identifiers []string, userID uint) (int64, error)
	// DeleteImageByIdentifierAndUser 根据标识符和用户ID删除图片
	DeleteImageByIdentifierAndUser(identifier string, userID uint) error
	// GetSoftDeletedImageByHash 获取软删除的图片（通过哈希）
	GetSoftDeletedImageByHash(hash string) (*models.Image, error)
	// UpdateImageByIdentifier 根据标识符更新图片
	UpdateImageByIdentifier(identifier string, updates map[string]interface{}) (*models.Image, error)
	// GetImageList 获取图片列表
	GetImageList(storageType, identifier, search string, albumID *uint, page, limit, userID int) ([]*models.Image, int64, error)
}

// 确保 Repository 实现了 RepositoryInterface
var _ RepositoryInterface = (*Repository)(nil)
