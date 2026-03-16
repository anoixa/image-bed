package albums

import (
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/albums"
)

type Service struct {
	repo *albums.Repository
}

type AlbumInfo = albums.AlbumInfo

func NewService(repo *albums.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetAlbumWithImagesByID(albumID, userID uint) (*models.Album, error) {
	return s.repo.GetAlbumWithImagesByID(albumID, userID)
}

func (s *Service) CreateAlbum(album *models.Album) error {
	return s.repo.CreateAlbum(album)
}

func (s *Service) UpdateAlbum(album *models.Album) error {
	return s.repo.UpdateAlbum(album)
}

func (s *Service) DeleteAlbum(albumID, userID uint) error {
	return s.repo.DeleteAlbum(albumID, userID)
}

func (s *Service) GetUserAlbums(userID uint, page, limit int) ([]*AlbumInfo, int64, error) {
	return s.repo.GetUserAlbums(userID, page, limit)
}

func (s *Service) AddImagesToAlbum(albumID, userID uint, imageIDs []uint) error {
	return s.repo.AddImagesToAlbum(albumID, userID, imageIDs)
}

func (s *Service) RemoveImageFromAlbum(albumID, userID uint, image *models.Image) error {
	return s.repo.RemoveImageFromAlbum(albumID, userID, image)
}

func (s *Service) RemoveImagesFromAlbum(albumID, userID uint, imageIDs []uint) (int64, error) {
	return s.repo.RemoveImagesFromAlbum(albumID, userID, imageIDs)
}
