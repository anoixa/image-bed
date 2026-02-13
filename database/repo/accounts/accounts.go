package accounts

import (
	"errors"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// CreateDefaultAdminUser Create default administrator user
func CreateDefaultAdminUser() {
	db := dbcore.GetDBInstance()
	var count int64

	// 检查是否已存在管理员用户
	if err := db.Model(&models.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		log.Fatalf("Failed to check admin user existence: %v", err)
	}

	if count == 0 {
		defaultPassword := "admin123"
		hashedPassword, err := cryptopackage.GenerateFromPassword(defaultPassword)
		if err != nil {
			log.Fatalf("Failed to hash default password: %v", err)
		}

		err = dbcore.Transaction(func(tx *gorm.DB) error {
			user := &models.User{
				Username: "admin",
				Password: hashedPassword,
			}

			if err := tx.Create(user).Error; err != nil {
				return fmt.Errorf("failed to create admin user: %w", err)
			}

			log.Printf("Created default admin user with ID: %d", user.ID)
			log.Printf("IMPORTANT: Please change the default admin password immediately!")

			return nil
		})

		if err != nil {
			log.Fatalf("Failed to create default admin user: %v", err)
		}
	} else {
		log.Println("Admin user already exists, skipping creation")
	}
}

// GetUserByUsername Get user by username
func GetUserByUsername(username string) (*models.User, error) {
	db := dbcore.GetDBInstance()
	var user models.User

	err := db.Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

// GetUserByUserIDWithCache Get User by id (缓存功能已移除，直接使用数据库查询)
func GetUserByUserIDWithCache(id uint) (*models.User, error) {
	db := dbcore.GetDBInstance()
	var user models.User

	err := db.Where("id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}
