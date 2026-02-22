package albums

import (
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/albums"
)

// Service 相册服务层
type Service struct {
	repo *albums.Repository
}

// AlbumInfo 相册信息（从 repository 透传）
type AlbumInfo = albums.AlbumInfo

// NewService 创建新的相册服务
func NewService(repo *albums.Repository) *Service {
	return &Service{repo: repo}
}

// GetAlbumWithImagesByID 获取相册及其图片（透传）
func (s *Service) GetAlbumWithImagesByID(albumID, userID uint) (*models.Album, error) {
	return s.repo.GetAlbumWithImagesByID(albumID, userID)
}

// CreateAlbum 创建相册（透传）
func (s *Service) CreateAlbum(album *models.Album) error {
	return s.repo.CreateAlbum(album)
}

// UpdateAlbum 更新相册（透传）
func (s *Service) UpdateAlbum(album *models.Album) error {
	return s.repo.UpdateAlbum(album)
}

// DeleteAlbum 删除相册（透传）
func (s *Service) DeleteAlbum(albumID, userID uint) error {
	return s.repo.DeleteAlbum(albumID, userID)
}

// GetUserAlbums 获取用户相册列表（透传）
func (s *Service) GetUserAlbums(userID uint, page, limit int) ([]*AlbumInfo, int64, error) {
	return s.repo.GetUserAlbums(userID, page, limit)
}

// AddImagesToAlbum 批量添加图片到相册（透传）
func (s *Service) AddImagesToAlbum(albumID, userID uint, imageIDs []uint) error {
	return s.repo.AddImagesToAlbum(albumID, userID, imageIDs)
}

// RemoveImageFromAlbum 从相册移除图片（透传）
func (s *Service) RemoveImageFromAlbum(albumID, userID uint, image *models.Image) error {
	return s.repo.RemoveImageFromAlbum(albumID, userID, image)
}
