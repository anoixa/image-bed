package key

import (
	"errors"
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

	// 使用 Preload("User") 来告诉 GORM 在查询 ApiToken 的同时，
	// 也要把关联的 User 对象一起查询出来并填充到 apiToken.User 字段中。
	// "User" 对应的是 ApiToken 结构体中的字段名。
	result := db.Preload("User").Where("token = ?", token).First(&apiToken)

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
