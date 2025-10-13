package key

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

func GetUserByApiToken(token string) (*models.User, error) {
	if token == "" {
		return nil, errors.New("invalid or non-existent API token")
	}

	db := dbcore.GetDBInstance()

	var apiToken models.ApiToken

	hasher := sha256.New()
	hasher.Write([]byte(token))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	result := db.Preload("User").Where("token = ? AND is_active = ?", hashedToken, true).First(&apiToken)

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

	go updateTokenLastUsed(db, apiToken.ID)

	return &apiToken.User, nil
}

func updateTokenLastUsed(db *gorm.DB, tokenID uint) {
	err := db.Model(&models.ApiToken{}).Where("id = ?", tokenID).Update("last_used_at", time.Now()).Error
	if err != nil {
		log.Printf("Failed to update last_used_at for token ID %d: %v", tokenID, err)
	}
}

func DeleteKey(key *models.ApiToken) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Delete(&key).Error
		if err != nil {
			return fmt.Errorf("failed to delete key in transaction: %w", err)
		}
		return err
	})
	return err
}

func CreateKey(key *models.ApiToken) error {
	err := dbcore.Transaction(func(tx *gorm.DB) error {
		err := tx.Create(&key).Error
		if err != nil {
			return fmt.Errorf("failed to create key in transaction: %w", err)
		}
		return err
	})
	return err
}

func GetAllApiTokensByUser(userID uint) ([]models.ApiToken, error) {
	if userID == 0 {
		return nil, errors.New("invalid user ID")
	}

	db := dbcore.GetDBInstance()
	var apiTokens []models.ApiToken

	result := db.Where("user_id = ?", userID).Order("created_at desc").Find(&apiTokens)

	if result.Error != nil {
		log.Printf("Database error while searching for all API tokens by user ID %d: %v", userID, result.Error)
		return nil, errors.New("database error")
	}

	return apiTokens, nil
}

func DisableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := dbcore.Transaction(func(tx *gorm.DB) error {
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

func RevokeApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := dbcore.Transaction(func(tx *gorm.DB) error {
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

func EnableApiToken(tokenID, userID uint) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("invalid token ID or user ID")
	}

	err := dbcore.Transaction(func(tx *gorm.DB) error {
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
