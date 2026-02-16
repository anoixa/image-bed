package accounts

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// Repository 账户仓库 - 封装所有账户相关的数据库操作
type Repository struct {
	db database.Provider
}

// NewRepository 创建新的账户仓库
func NewRepository(db database.Provider) *Repository {
	return &Repository{db: db}
}

// DB 返回底层数据库连接
func (r *Repository) DB() *gorm.DB {
	return r.db.DB()
}

// CreateDefaultAdminUser 创建默认管理员用户
func (r *Repository) CreateDefaultAdminUser() {
	var count int64

	// 检查是否已存在管理员用户
	if err := r.db.DB().Model(&models.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		log.Fatalf("Failed to check admin user existence: %v", err)
	}

	if count == 0 {
		// random password generation
		randomPassword, err := utils.GenerateRandomToken(16)
		if err != nil {
			log.Fatalf("Failed to generate random password: %v", err)
		}

		if len(randomPassword) > 16 {
			randomPassword = randomPassword[:16]
		}

		hashedPassword, err := cryptopackage.GenerateFromPassword(randomPassword)
		if err != nil {
			log.Fatalf("Failed to hash default password: %v", err)
		}

		err = r.db.Transaction(func(tx *gorm.DB) error {
			user := &models.User{
				Username: "admin",
				Password: hashedPassword,
				Role:     models.RoleAdmin,
			}

			if err := tx.Create(user).Error; err != nil {
				return fmt.Errorf("failed to create admin user: %w", err)
			}

			log.Println("========================================")
			log.Println("🎉 默认管理员用户创建成功")
			log.Printf("   用户名: admin")
			log.Printf("   密码: %s", randomPassword)
			log.Println("========================================")
			log.Println("⚠️  请登录后立即修改默认密码！")

			return nil
		})

		if err != nil {
			log.Fatalf("Failed to create default admin user: %v", err)
		}
	} else {
		log.Println("Admin user already exists, skipping creation")
	}
}

// GetUserByUsername 通过用户名获取用户
func (r *Repository) GetUserByUsername(username string) (*models.User, error) {
	var user models.User

	err := r.db.DB().Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

// GetUserByID 通过ID获取用户
func (r *Repository) GetUserByID(id uint) (*models.User, error) {
	var user models.User

	err := r.db.DB().Where("id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

// CreateUser 创建用户
func (r *Repository) CreateUser(user *models.User) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		return nil
	})
}

// UpdateUser 更新用户
func (r *Repository) UpdateUser(user *models.User) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Save(user).Error
	})
}

// DeleteUser 删除用户
func (r *Repository) DeleteUser(userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&models.User{}, userID).Error
	})
}

// UserExists 检查用户是否存在
func (r *Repository) UserExists(username string) (bool, error) {
	var count int64
	err := r.db.DB().Model(&models.User{}).Where("username = ?", username).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAllUsers 获取所有用户
func (r *Repository) GetAllUsers(page, pageSize int) ([]*models.User, int64, error) {
	var users []*models.User
	var total int64

	db := r.db.DB().Model(&models.User{})

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&users).Error
	return users, total, err
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
