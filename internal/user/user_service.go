package user

import (
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/repo/accounts"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
)

// Service 用户服务
type Service struct {
	accountsRepo *accounts.Repository
	devicesRepo  *accounts.DeviceRepository
}

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidOldPassword = errors.New("invalid old password")
	ErrSamePassword       = errors.New("new password cannot be the same as old password")
)

// NewService 创建新的用户服务
func NewService(accountsRepo *accounts.Repository, devicesRepo *accounts.DeviceRepository) *Service {
	return &Service{
		accountsRepo: accountsRepo,
		devicesRepo:  devicesRepo,
	}
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	UserID      uint
	OldPassword string
	NewPassword string
}

// ChangePassword 修改用户密码
func (s *Service) ChangePassword(req ChangePasswordRequest) error {
	// 获取用户信息
	user, err := s.accountsRepo.GetUserByID(req.UserID)
	if err != nil {
		if errors.Is(err, accounts.ErrUserNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("failed to get user: %w", err)
	}

	// 验证旧密码
	ok, err := cryptopackage.ComparePasswordAndHash(req.OldPassword, user.Password)
	if err != nil {
		return fmt.Errorf("password comparison failed: %w", err)
	}
	if !ok {
		return ErrInvalidOldPassword
	}

	if req.OldPassword == req.NewPassword {
		return ErrSamePassword
	}

	// 生成新密码哈希
	hashedPassword, err := cryptopackage.GenerateFromPassword(req.NewPassword)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	// 更新密码
	if err := s.accountsRepo.UpdatePassword(req.UserID, hashedPassword); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	if s.devicesRepo != nil {
		if err := s.devicesRepo.DeleteDevicesByUser(req.UserID); err != nil {
			return fmt.Errorf("failed to revoke user sessions: %w", err)
		}
	}

	return nil
}
