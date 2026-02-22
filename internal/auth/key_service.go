package auth

import (
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/keys"
)

// KeyService API Token 服务层
type KeyService struct {
	repo *keys.Repository
}

// NewKeyService 创建新的 API Token 服务
func NewKeyService(repo *keys.Repository) *KeyService {
	return &KeyService{repo: repo}
}

// GetAllApiTokensByUser 获取用户的所有 API Token
func (s *KeyService) GetAllApiTokensByUser(userID uint) ([]models.ApiToken, error) {
	return s.repo.GetAllApiTokensByUser(userID)
}

// CreateKey 创建 API Token
func (s *KeyService) CreateKey(token *models.ApiToken) error {
	return s.repo.CreateKey(token)
}

// DisableApiToken 禁用 API Token
func (s *KeyService) DisableApiToken(tokenID, userID uint) error {
	return s.repo.DisableApiToken(tokenID, userID)
}

// EnableApiToken 启用 API Token
func (s *KeyService) EnableApiToken(tokenID, userID uint) error {
	return s.repo.EnableApiToken(tokenID, userID)
}

// RevokeApiToken 撤销 API Token
func (s *KeyService) RevokeApiToken(tokenID, userID uint) error {
	return s.repo.RevokeApiToken(tokenID, userID)
}
