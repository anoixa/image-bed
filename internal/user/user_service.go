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
}

// NewService 创建新的用户服务
func NewService(accountsRepo *accounts.Repository) *Service {
	return &Service{
		accountsRepo: accountsRepo,
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
			return errors.New("user not found")
		}
		return fmt.Errorf("failed to get user: %w", err)
	}

	// 验证旧密码
	ok, err := cryptopackage.ComparePasswordAndHash(req.OldPassword, user.Password)
	if err != nil {
		return fmt.Errorf("password comparison failed: %w", err)
	}
	if !ok {
		return errors.New("invalid old password")
	}

	if req.OldPassword == req.NewPassword {
		return errors.New("new password cannot be the same as old password")
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

	return nil
}
