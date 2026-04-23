package admin

import (
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	albumRepo "github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/utils"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var adminLog = utils.ForModule("AdminUser")

var (
	ErrUsernameEmpty     = errors.New("username cannot be empty")
	ErrPasswordTooShort  = errors.New("password must be at least 6 characters")
	ErrUsernameExists    = errors.New("username already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrUserHasOwnedData  = errors.New("cannot delete user with owned images or albums")
	ErrLastAdmin         = errors.New("cannot modify or delete the last admin")
	ErrCannotDisableSelf = errors.New("cannot disable yourself")
	ErrInvalidRole       = errors.New("invalid role")
)

const minPasswordLength = 6

// UserService 管理员用户管理服务
type UserService struct {
	accountsRepo *accounts.Repository
	devicesRepo  *accounts.DeviceRepository
	keysRepo     *keys.Repository
	imagesRepo   *images.Repository
	albumsRepo   *albumRepo.Repository
}

// NewUserService 创建用户管理服务
func NewUserService(
	accountsRepo *accounts.Repository,
	devicesRepo *accounts.DeviceRepository,
	keysRepo *keys.Repository,
	imagesRepo *images.Repository,
	albumsRepo *albumRepo.Repository,
) *UserService {
	return &UserService{
		accountsRepo: accountsRepo,
		devicesRepo:  devicesRepo,
		keysRepo:     keysRepo,
		imagesRepo:   imagesRepo,
		albumsRepo:   albumsRepo,
	}
}

// CreateUser 创建新用户（管理员操作）
// If password is empty, a random password is generated and returned.
func (s *UserService) CreateUser(username, password, role string) (*models.User, string, error) {
	if username == "" {
		return nil, "", ErrUsernameEmpty
	}

	exists, err := s.accountsRepo.UserExists(username)
	if err != nil {
		return nil, "", fmt.Errorf("failed to check username: %w", err)
	}
	if exists {
		return nil, "", ErrUsernameExists
	}

	generatedPassword := ""
	if password == "" {
		generatedPassword, err = utils.GenerateRandomToken(16)
		if err != nil {
			return nil, "", fmt.Errorf("failed to generate password: %w", err)
		}
		password = generatedPassword
	}

	if len(password) < minPasswordLength {
		return nil, "", ErrPasswordTooShort
	}

	if role == "" {
		role = models.RoleUser
	}
	if role != models.RoleUser && role != models.RoleAdmin {
		return nil, "", ErrInvalidRole
	}

	hashedPassword, err := cryptopackage.GenerateFromPassword(password)
	if err != nil {
		return nil, "", fmt.Errorf("failed to hash password: %w", err)
	}

	user := &models.User{
		Username: username,
		Password: hashedPassword,
		Role:     role,
		Status:   models.UserStatusActive,
	}

	if err := s.accountsRepo.CreateUser(user); err != nil {
		return nil, "", fmt.Errorf("failed to create user: %w", err)
	}

	adminLog.Infof("Admin created user: %s (role: %s)", username, role)
	return user, generatedPassword, nil
}

// UpdateRole 更新用户角色
func (s *UserService) UpdateRole(userID uint, role string) error {
	return s.withTx(func(tx *gorm.DB) error {
		user, err := loadUserForUpdate(tx, userID)
		if err != nil {
			return err
		}

		// Demoting the last active admin to user must be blocked.
		if user.Role == models.RoleAdmin && user.IsActive() && role != models.RoleAdmin {
			isLast, err := isLastActiveAdminTx(tx, user)
			if err != nil {
				return err
			}
			if isLast {
				return ErrLastAdmin
			}
		}

		return tx.Model(&models.User{}).Where("id = ?", userID).Update("role", role).Error
	})
}

// UpdateStatus 更新用户状态
func (s *UserService) UpdateStatus(userID uint, status string) error {
	return s.withTx(func(tx *gorm.DB) error {
		user, err := loadUserForUpdate(tx, userID)
		if err != nil {
			return err
		}

		if status == models.UserStatusDisabled && user.Role == models.RoleAdmin && user.IsActive() {
			isLast, err := isLastActiveAdminTx(tx, user)
			if err != nil {
				return err
			}
			if isLast {
				return ErrLastAdmin
			}
		}

		if status == models.UserStatusDisabled {
			if s.devicesRepo != nil {
				if err := accounts.NewDeviceRepository(tx).DeleteDevicesByUser(userID); err != nil {
					return err
				}
			}
			if s.keysRepo != nil {
				if err := keys.NewRepository(tx).DisableTokensByUser(userID); err != nil {
					return err
				}
			}
		}

		return tx.Model(&models.User{}).Where("id = ?", userID).Update("status", status).Error
	})
}

// ResetPassword 重置用户密码，返回新密码
func (s *UserService) ResetPassword(userID uint) (string, error) {
	_, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		return "", ErrUserNotFound
	}

	newPassword, err := utils.GenerateRandomToken(16)
	if err != nil {
		return "", fmt.Errorf("failed to generate password: %w", err)
	}

	hashedPassword, err := cryptopackage.GenerateFromPassword(newPassword)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	if err := s.accountsRepo.UpdatePassword(userID, hashedPassword); err != nil {
		return "", fmt.Errorf("failed to update password: %w", err)
	}

	// Clear all sessions
	if s.devicesRepo != nil {
		_ = s.devicesRepo.DeleteDevicesByUser(userID)
	}

	adminLog.Infof("Admin reset password for user ID: %d", userID)
	return newPassword, nil
}

// DeleteUser 删除用户
func (s *UserService) DeleteUser(userID uint) error {
	return s.withTx(func(tx *gorm.DB) error {
		user, err := loadUserForUpdate(tx, userID)
		if err != nil {
			return err
		}

		if user.Role == models.RoleAdmin && user.IsActive() {
			isLast, err := isLastActiveAdminTx(tx, user)
			if err != nil {
				return err
			}
			if isLast {
				return ErrLastAdmin
			}
		}

		// Refuse deletion if the user still owns content.
		if s.imagesRepo != nil {
			count, err := images.NewRepository(tx).CountImagesByUser(userID)
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrUserHasOwnedData
			}
		}
		if s.albumsRepo != nil {
			count, err := albumRepo.NewRepository(tx).CountAlbumsByUser(userID)
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrUserHasOwnedData
			}
		}
		if s.devicesRepo != nil {
			if err := accounts.NewDeviceRepository(tx).DeleteDevicesByUser(userID); err != nil {
				return err
			}
		}
		if s.keysRepo != nil {
			if err := keys.NewRepository(tx).DeleteTokensByUser(userID); err != nil {
				return err
			}
		}
		return tx.Delete(&models.User{}, userID).Error
	})
}

// ListUsers 获取用户列表
func (s *UserService) ListUsers(page, pageSize int) ([]*models.User, int64, error) {
	return s.accountsRepo.GetAllUsers(page, pageSize)
}

// isLastAdmin 检查指定用户是否是最后一个管理员
func (s *UserService) isLastAdmin(userID uint) (bool, error) {
	user, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		return false, err
	}
	if user.Role != models.RoleAdmin {
		return false, nil
	}

	adminCount, err := s.accountsRepo.CountActiveAdmins()
	if err != nil {
		return false, err
	}

	return adminCount <= 1, nil
}

func (s *UserService) withTx(fn func(tx *gorm.DB) error) error {
	if s.accountsRepo == nil || s.accountsRepo.DB() == nil {
		return fmt.Errorf("accounts repository not initialized")
	}

	return s.accountsRepo.DB().Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

func loadUserForUpdate(tx *gorm.DB, userID uint) (*models.User, error) {
	var user models.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", userID).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func isLastActiveAdminTx(tx *gorm.DB, user *models.User) (bool, error) {
	if user == nil || user.Role != models.RoleAdmin || !user.IsActive() {
		return false, nil
	}

	var admins []models.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("role = ? AND status = ?", models.RoleAdmin, models.UserStatusActive).
		Find(&admins).Error; err != nil {
		return false, err
	}

	return len(admins) <= 1, nil
}
