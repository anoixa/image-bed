package keys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

// Repository API Token 仓库 - 封装所有 Token 相关的数据库操作
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的 Token 仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetUserByApiToken 通过 API Token 获取用户
func (r *Repository) GetUserByApiToken(token string) (*models.User, error) {
	if token == "" {
		return nil, errors.New("invalid or non-existent API token")
	}

	hasher := sha256.New()
	hasher.Write([]byte(token))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	var apiToken models.ApiToken
	result := r.db.Preload("User").Where("token = ? AND is_active = ?", hashedToken, true).First(&apiToken)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid or non-existent API token")
		}
		return nil, result.Error
	}

	if apiToken.User.ID == 0 {
		return nil, errors.New("invalid or non-existent API token")
	}

	utils.SafeGo(func() {
		r.updateTokenLastUsed(apiToken.ID)
	})
	return &apiToken.User, nil
}

// updateTokenLastUsed 更新 Token 最后使用时间
func (r *Repository) updateTokenLastUsed(tokenID uint) {
	ctx := context.Background()
	err := r.db.WithContext(ctx).Model(&models.ApiToken{}).Where("id = ?", tokenID).Update("last_used_at", time.Now()).Error
	if err != nil {
		log.Printf("Failed to update last_used_at for token ID %d: %v", tokenID, err)
	}
}

// DeleteKey 删除 Token
func (r *Repository) DeleteKey(key *models.ApiToken) error {
	return r.db.Delete(&key).Error
}

// CreateKey 创建 Token
func (r *Repository) CreateKey(key *models.ApiToken) error {
	return r.db.Create(&key).Error
}

// GetAllApiTokensByUser 获取用户的所有 API Token
func (r *Repository) GetAllApiTokensByUser(userID uint) ([]models.ApiToken, error) {
	if userID == 0 {
		return nil, errors.New("invalid user ID")
	}

	var apiTokens []models.ApiToken
	result := r.db.Where("user_id = ?", userID).Order("created_at desc").Find(&apiTokens)
	return apiTokens, result.Error
}

// DisableApiToken 禁用 API Token
func (r *Repository) DisableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	result := r.db.Model(&models.ApiToken{}).Where("id = ? AND user_id = ?", tokenID, userID).Update("is_active", false)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// RevokeApiToken 撤销 API Token（删除）
func (r *Repository) RevokeApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	result := r.db.Where("id = ? AND user_id = ?", tokenID, userID).Delete(&models.ApiToken{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// EnableApiToken 启用 API Token
func (r *Repository) EnableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	result := r.db.Model(&models.ApiToken{}).Where("id = ? AND user_id = ?", tokenID, userID).Update("is_active", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetTokenByID 通过 ID 获取 Token
func (r *Repository) GetTokenByID(tokenID uint) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.First(&token, tokenID).Error
	return &token, err
}

// GetTokenByIDAndUser 通过 ID 和用户 ID 获取 Token
func (r *Repository) GetTokenByIDAndUser(tokenID, userID uint) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error
	return &token, err
}

// TokenExists 检查 Token 是否存在
func (r *Repository) TokenExists(tokenID uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.ApiToken{}).Where("id = ?", tokenID).Count(&count).Error
	return count > 0, err
}

// CountTokensByUser 统计用户的 Token 数量
func (r *Repository) CountTokensByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.ApiToken{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// UpdateToken 更新 Token
func (r *Repository) UpdateToken(token *models.ApiToken) error {
	return r.db.Save(token).Error
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}
