package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/internal/auth"
	"github.com/anoixa/image-bed/internal/mfa"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service 用户服务
type Service struct {
	accountsRepo  *accounts.Repository
	devicesRepo   *accounts.DeviceRepository
	twoFactorRepo *accounts.TwoFactorRepository
	secretCrypto  auth.SecretEncryptor
}

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidOldPassword = errors.New("invalid old password")
	ErrSamePassword       = errors.New("new password cannot be the same as old password")
	Err2FAAlreadyEnabled  = errors.New("2FA already enabled")
	Err2FANotSetup        = errors.New("2FA not set up")
	ErrInvalid2FACode     = errors.New("invalid 2FA code")
	Err2FAServiceDisabled = errors.New("2FA service not initialized")
	Err2FAReplay          = auth.ErrTwoFactorReplay
)

const pendingTOTPSecretTTL = 10 * time.Minute

// NewService 创建新的用户服务
func NewService(accountsRepo *accounts.Repository, devicesRepo *accounts.DeviceRepository) *Service {
	return &Service{
		accountsRepo: accountsRepo,
		devicesRepo:  devicesRepo,
	}
}

func NewServiceWith2FA(
	accountsRepo *accounts.Repository,
	devicesRepo *accounts.DeviceRepository,
	twoFactorRepo *accounts.TwoFactorRepository,
	secretCrypto auth.SecretEncryptor,
) *Service {
	s := NewService(accountsRepo, devicesRepo)
	s.twoFactorRepo = twoFactorRepo
	s.secretCrypto = secretCrypto
	return s
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	UserID      uint
	OldPassword string
	NewPassword string
}

// CurrentUser 当前用户信息
type CurrentUser struct {
	ID       uint
	Username string
	Role     string
	Status   string
}

// GetCurrentUser 获取当前登录用户的最小资料
func (s *Service) GetCurrentUser(userID uint) (*CurrentUser, error) {
	user, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, accounts.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &CurrentUser{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
		Status:   user.Status,
	}, nil
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

// TOTPSetupResult TOTP setup 返回结果
type TOTPSetupResult struct {
	Enabled bool   `json:"enabled"`
	URI     string `json:"uri,omitempty"`
	Secret  string `json:"secret,omitempty"`
}

type Setup2FARequest struct {
	UserID          uint
	CurrentPassword string
	CurrentCode     string
}

// Get2FAStatus 获取用户 2FA 状态
func (s *Service) Get2FAStatus(userID uint) (bool, error) {
	if s.twoFactorRepo == nil {
		return false, nil
	}
	enabled, err := s.twoFactorRepo.IsTOTPEnabled(context.Background(), userID)
	if err != nil {
		return false, fmt.Errorf("failed to get 2FA status: %w", err)
	}
	return enabled, nil
}

// Setup2FA 生成 TOTP secret，返回 URI（不立即启用）
func (s *Service) Setup2FA(req Setup2FARequest) (*TOTPSetupResult, error) {
	if s.twoFactorRepo == nil || s.secretCrypto == nil {
		return nil, Err2FAServiceDisabled
	}

	user, err := s.accountsRepo.GetUserByID(req.UserID)
	if err != nil {
		if errors.Is(err, accounts.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	enabled, err := s.twoFactorRepo.IsTOTPEnabled(context.Background(), req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get 2FA status: %w", err)
	}
	if enabled {
		if err := s.verifyCurrentTOTP(req.UserID, req.CurrentCode); err != nil {
			return nil, err
		}
	} else {
		ok, err := cryptopackage.ComparePasswordAndHash(req.CurrentPassword, user.Password)
		if err != nil {
			return nil, fmt.Errorf("password comparison failed: %w", err)
		}
		if !ok {
			return nil, ErrInvalidOldPassword
		}
	}

	secret, uri, err := mfa.GenerateTOTPSecret(user.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	encrypted, err := s.secretCrypto.EncryptString(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt TOTP secret: %w", err)
	}

	expiresAt := time.Now().Add(pendingTOTPSecretTTL)
	if _, err := s.twoFactorRepo.SavePendingTOTP(context.Background(), req.UserID, encrypted, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to save TOTP secret: %w", err)
	}

	return &TOTPSetupResult{
		Enabled: false,
		URI:     uri,
		Secret:  secret,
	}, nil
}

// Enable2FA 验证 code 后正式启用 2FA
func (s *Service) Enable2FA(userID uint, code string) error {
	if s.twoFactorRepo == nil || s.secretCrypto == nil {
		return Err2FAServiceDisabled
	}
	now := time.Now()
	return s.twoFactorRepo.DB().WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		var setting models.UserTOTPSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).
			First(&setting).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return Err2FANotSetup
			}
			return err
		}
		if setting.Enabled {
			return Err2FAAlreadyEnabled
		}
		if setting.PendingSecretEncrypted == "" || setting.PendingExpiresAt == nil || now.After(*setting.PendingExpiresAt) {
			return Err2FANotSetup
		}

		secret, err := s.secretCrypto.DecryptString(setting.PendingSecretEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt pending TOTP secret: %w", err)
		}
		step, ok := mfa.ValidateTOTPCode(secret, code, now)
		if !ok {
			return ErrInvalid2FACode
		}

		enabledAt := now
		return accounts.NewTwoFactorRepository(tx).EnableTOTP(context.Background(), userID, setting.PendingSecretEncrypted, enabledAt, step)
	})
}

// Disable2FA 验证 code 后关闭 2FA
func (s *Service) Disable2FA(userID uint, code string) error {
	if s.twoFactorRepo == nil || s.secretCrypto == nil {
		return Err2FAServiceDisabled
	}

	if err := s.verifyCurrentTOTP(userID, code); err != nil {
		return err
	}

	if err := s.twoFactorRepo.ResetTOTP(context.Background(), userID); err != nil {
		return fmt.Errorf("failed to disable 2FA: %w", err)
	}

	return nil
}

func (s *Service) verifyCurrentTOTP(userID uint, code string) error {
	now := time.Now()
	return s.twoFactorRepo.DB().WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		var setting models.UserTOTPSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND enabled = ? AND secret_encrypted <> ''", userID, true).
			First(&setting).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return Err2FANotSetup
			}
			return err
		}

		secret, err := s.secretCrypto.DecryptString(setting.SecretEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt TOTP secret: %w", err)
		}
		step, ok := mfa.ValidateTOTPCode(secret, code, now)
		if !ok {
			return ErrInvalid2FACode
		}
		if step <= setting.LastUsedStep {
			return Err2FAReplay
		}

		return tx.Model(&setting).Update("last_used_step", step).Error
	})
}
