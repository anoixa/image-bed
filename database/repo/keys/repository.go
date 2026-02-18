package keys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Repository API Token 仓库 - 封装所有 Token 相关的数据库操作
type Repository struct {
	db database.Provider
}

// NewRepository 创建新的 Token 仓库
func NewRepository(db database.Provider) *Repository {
	return &Repository{db: db}
}

// GetUserByApiToken 通过 API Token 获取用户
func (r *Repository) GetUserByApiToken(token string) (*models.User, error) {
	if token == "" {
		return nil, errors.New("invalid or non-existent API token")
	}

	var apiToken models.ApiToken

	hasher := sha256.New()
	hasher.Write([]byte(token))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	result := r.db.DB().Preload("User").Where("token = ? AND is_active = ?", hashedToken, true).First(&apiToken)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid or non-existent API token")
		}
		log.Printf("Database error while searching for API token: %v", result.Error)
		return nil, result.Error
	}

	if apiToken.User.ID == 0 {
		log.Printf("Orphaned API token detected. Token ID: %d, UserID: %d does not exist.", apiToken.ID, apiToken.UserID)
		return nil, errors.New("invalid or non-existent API token")
	}

	go r.updateTokenLastUsed(apiToken.ID)

	return &apiToken.User, nil
}

// updateTokenLastUsed 更新 Token 最后使用时间
func (r *Repository) updateTokenLastUsed(tokenID uint) {
	err := r.db.DB().Model(&models.ApiToken{}).Where("id = ?", tokenID).Update("last_used_at", time.Now()).Error
	if err != nil {
		log.Printf("Failed to update last_used_at for token ID %d: %v", tokenID, err)
	}
}

// DeleteKey 删除 Token
func (r *Repository) DeleteKey(key *models.ApiToken) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Delete(&key).Error
		if err != nil {
			return fmt.Errorf("failed to delete key in transaction: %w", err)
		}
		return err
	})
}

// CreateKey 创建 Token
func (r *Repository) CreateKey(key *models.ApiToken) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Create(&key).Error
		if err != nil {
			return fmt.Errorf("failed to create key in transaction: %w", err)
		}
		return err
	})
}

// GetAllApiTokensByUser 获取用户的所有 API Token
func (r *Repository) GetAllApiTokensByUser(userID uint) ([]models.ApiToken, error) {
	if userID == 0 {
		return nil, errors.New("invalid user ID")
	}

	var apiTokens []models.ApiToken

	result := r.db.DB().Where("user_id = ?", userID).Order("created_at desc").Find(&apiTokens)

	if result.Error != nil {
		log.Printf("Database error while searching for all API tokens by user ID %d: %v", userID, result.Error)
		return nil, errors.New("database error")
	}

	return apiTokens, nil
}

// DisableApiToken 禁用 API Token
func (r *Repository) DisableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ApiToken{}).
			Where("id = ? AND user_id = ?", tokenID, userID).
			Update("is_active", false)

		if result.Error != nil {
			log.Printf("Database error while disabling token ID %d for user ID %d: %v", tokenID, userID, result.Error)
			return fmt.Errorf("failed to disable token: %w", result.Error)
		}

		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})

	return err
}

// RevokeApiToken 撤销 API Token（删除）
func (r *Repository) RevokeApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND user_id = ?", tokenID, userID).
			Delete(&models.ApiToken{})

		if result.Error != nil {
			log.Printf("Database error while revoking token ID %d for user ID %d: %v", tokenID, userID, result.Error)
			return fmt.Errorf("failed to revoke token: %w", result.Error)
		}

		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})

	return err
}

// EnableApiToken 启用 API Token
func (r *Repository) EnableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ApiToken{}).
			Where("id = ? AND user_id = ?", tokenID, userID).
			Update("is_active", true)

		if result.Error != nil {
			log.Printf("Database error while enabling token ID %d for user ID %d: %v", tokenID, userID, result.Error)
			return fmt.Errorf("failed to enable token: %w", result.Error)
		}

		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})

	return err
}

// GetTokenByID 通过 ID 获取 Token
func (r *Repository) GetTokenByID(tokenID uint) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.DB().First(&token, tokenID).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// GetTokenByIDAndUser 通过 ID 和用户 ID 获取 Token
func (r *Repository) GetTokenByIDAndUser(tokenID, userID uint) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.DB().Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// TokenExists 检查 Token 是否存在
func (r *Repository) TokenExists(tokenID uint) (bool, error) {
	var count int64
	err := r.db.DB().Model(&models.ApiToken{}).Where("id = ?", tokenID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountTokensByUser 统计用户的 Token 数量
func (r *Repository) CountTokensByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.DB().Model(&models.ApiToken{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// UpdateToken 更新 Token
func (r *Repository) UpdateToken(token *models.ApiToken) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Save(token).Error
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
