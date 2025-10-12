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
