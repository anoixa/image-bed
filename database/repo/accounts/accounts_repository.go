package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

// Repository 账户仓库
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的账户仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB 返回底层数据库连接
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// CreateDefaultAdminUser 创建默认管理员用户
// 返回可能的错误，让调用者决定是否终止程序
func (r *Repository) CreateDefaultAdminUser() (string, error) {
	var count int64

	if err := r.db.Model(&models.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		return "", fmt.Errorf("failed to check admin user existence: %w", err)
	}

	if count == 0 {
		randomPassword, err := utils.GenerateRandomToken(16)
		if err != nil {
			return "", fmt.Errorf("failed to generate random password: %w", err)
		}

		hashedPassword, err := cryptopackage.GenerateFromPassword(randomPassword)
		if err != nil {
			return "", fmt.Errorf("failed to hash default password: %w", err)
		}

		user := &models.User{
			Username: "admin",
			Password: hashedPassword,
			Role:     models.RoleAdmin,
		}

		if err := r.db.Create(user).Error; err != nil {
			return "", fmt.Errorf("failed to create default admin user: %w", err)
		}

		return randomPassword, nil
	}

	return "", nil
}

// ErrUserNotFound 用户不存在错误
var ErrUserNotFound = errors.New("user not found")

// GetUserByUsername 通过用户名获取用户
func (r *Repository) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := r.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByID 通过ID获取用户
func (r *Repository) GetUserByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// CreateUser 创建用户
func (r *Repository) CreateUser(user *models.User) error {
	return r.db.Create(user).Error
}

// UpdateUser 更新用户
func (r *Repository) UpdateUser(user *models.User) error {
	return r.db.Save(user).Error
}

// DeleteUser 删除用户
func (r *Repository) DeleteUser(userID uint) error {
	return r.db.Delete(&models.User{}, userID).Error
}

// UserExists 检查用户是否存在
func (r *Repository) UserExists(username string) (bool, error) {
	var count int64
	err := r.db.Model(&models.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

// GetAllUsers 获取所有用户
func (r *Repository) GetAllUsers(page, pageSize int) ([]*models.User, int64, error) {
	var users []*models.User
	var total int64

	db := r.db.Model(&models.User{})
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := db.Order("created_at desc").Offset(offset).Limit(pageSize).Find(&users).Error
	return users, total, err
}

// WithContext 返回带上下文的仓库
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}
