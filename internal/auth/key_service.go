package auth

import (
	"errors"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/keys"
)

var ErrUserDisabled = errors.New("user account disabled")

// KeyService API Token 服务层
type KeyService struct {
	repo         *keys.Repository
	accountsRepo *accounts.Repository
}

// NewKeyService 创建新的 API Token 服务
func NewKeyService(repo *keys.Repository, accountsRepo *accounts.Repository) *KeyService {
	return &KeyService{repo: repo, accountsRepo: accountsRepo}
}

func (s *KeyService) ensureActiveUser(userID uint) error {
	if s.accountsRepo == nil {
		return nil
	}

	user, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil || !user.IsActive() {
		return ErrUserDisabled
	}
	return nil
}

// GetAllApiTokensByUser 获取用户的所有 API Token
func (s *KeyService) GetAllApiTokensByUser(userID uint) ([]models.ApiToken, error) {
	if err := s.ensureActiveUser(userID); err != nil {
		return nil, err
	}
	return s.repo.GetAllApiTokensByUser(userID)
}

// CreateKey 创建 API Token
func (s *KeyService) CreateKey(token *models.ApiToken) error {
	if token == nil {
		return errors.New("token is nil")
	}
	if err := s.ensureActiveUser(token.UserID); err != nil {
		return err
	}
	return s.repo.CreateKey(token)
}

// DisableApiToken 禁用 API Token
func (s *KeyService) DisableApiToken(tokenID, userID uint) error {
	if err := s.ensureActiveUser(userID); err != nil {
		return err
	}
	return s.repo.DisableApiToken(tokenID, userID)
}

// EnableApiToken 启用 API Token
func (s *KeyService) EnableApiToken(tokenID, userID uint) error {
	if err := s.ensureActiveUser(userID); err != nil {
		return err
	}
	return s.repo.EnableApiToken(tokenID, userID)
}

// RevokeApiToken 撤销 API Token
func (s *KeyService) RevokeApiToken(tokenID, userID uint) error {
	if err := s.ensureActiveUser(userID); err != nil {
		return err
	}
	return s.repo.RevokeApiToken(tokenID, userID)
}
