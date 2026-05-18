package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/internal/mfa"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	twoFATicketTTL   = 5 * time.Minute
	twoFAMaxAttempts = 5
)

var (
	ErrInvalidTwoFactorTicket  = errors.New("invalid or expired 2FA ticket")
	ErrInvalidTwoFactorCode    = errors.New("invalid 2FA code")
	ErrTooManyTwoFactorAttempt = errors.New("too many attempts, ticket revoked")
	ErrTwoFactorReplay         = errors.New("2FA code already used")
)

type SecretEncryptor interface {
	EncryptString(plaintext string) (string, error)
	DecryptString(ciphertext string) (string, error)
}

// LoginResult 登录结果
type LoginResult struct {
	User               *models.User
	AccessToken        string
	AccessTokenExpiry  time.Time
	RefreshToken       string
	RefreshTokenExpiry time.Time
	DeviceID           string
	Requires2FA        bool   // 是否需要两步验证
	TwoFATicket        string // 临时票据
	TwoFAExpiresIn     int64  // 票据剩余有效秒数
}

// RefreshResult Token 刷新结果
type RefreshResult struct {
	AccessToken        string
	AccessTokenExpiry  time.Time
	RefreshToken       string
	RefreshTokenExpiry time.Time
	DeviceID           string
}

// LoginService 登录服务
type LoginService struct {
	accountsRepo  *accounts.Repository
	devicesRepo   *accounts.DeviceRepository
	jwtService    *JWTService
	twoFactorRepo *accounts.TwoFactorRepository
	secretCrypto  SecretEncryptor
}

// NewLoginService 创建新的登录服务
func NewLoginService(
	accountsRepo *accounts.Repository,
	devicesRepo *accounts.DeviceRepository,
	jwtService *JWTService,
) *LoginService {
	return &LoginService{
		accountsRepo: accountsRepo,
		devicesRepo:  devicesRepo,
		jwtService:   jwtService,
	}
}

func NewLoginServiceWith2FA(
	accountsRepo *accounts.Repository,
	devicesRepo *accounts.DeviceRepository,
	jwtService *JWTService,
	twoFactorRepo *accounts.TwoFactorRepository,
	secretCrypto SecretEncryptor,
) *LoginService {
	s := NewLoginService(accountsRepo, devicesRepo, jwtService)
	s.twoFactorRepo = twoFactorRepo
	s.secretCrypto = secretCrypto
	return s
}

// ValidateCredentials 验证用户凭据
func (s *LoginService) ValidateCredentials(username, password string) (*models.User, bool, error) {
	user, err := s.accountsRepo.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, accounts.ErrUserNotFound) {
			return nil, false, fmt.Errorf("invalid credentials")
		}
		return nil, false, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		return nil, false, fmt.Errorf("invalid credentials")
	}

	ok, err := cryptopackage.ComparePasswordAndHash(password, user.Password)
	if err != nil {
		return nil, false, fmt.Errorf("password comparison failed: %w", err)
	}

	if !ok {
		return nil, false, fmt.Errorf("invalid credentials")
	}

	return user, true, nil
}

// Login 执行登录操作
func (s *LoginService) Login(username, password string) (*LoginResult, error) {
	user, valid, err := s.ValidateCredentials(username, password)
	if err != nil {
		return nil, fmt.Errorf("failed to validate credentials: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid credentials")
	}

	return s.issuePasswordSessionOr2FAChallenge(context.Background(), user)
}

// issuePasswordSessionOr2FAChallenge issues a session unless password login requires TOTP.
func (s *LoginService) issuePasswordSessionOr2FAChallenge(ctx context.Context, user *models.User) (*LoginResult, error) {
	if !user.IsActive() {
		return nil, fmt.Errorf("account disabled")
	}

	if s.twoFactorRepo != nil && s.secretCrypto != nil {
		enabled, err := s.twoFactorRepo.IsTOTPEnabled(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check 2FA status: %w", err)
		}
		if enabled {
			ticket, expiresIn, err := s.Create2FAChallenge(ctx, user.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to create 2FA challenge: %w", err)
			}
			return &LoginResult{
				User:           user,
				Requires2FA:    true,
				TwoFATicket:    ticket,
				TwoFAExpiresIn: expiresIn,
			}, nil
		}
	}

	return s.IssueSessionForUser(user)
}

// IssueSessionForUser creates a new session (tokens + device) for an active user.
// It rejects disabled users and can be used by both password login and OAuth login.
func (s *LoginService) IssueSessionForUser(user *models.User) (*LoginResult, error) {
	if !user.IsActive() {
		return nil, fmt.Errorf("account disabled")
	}

	// 生成 tokens
	tokenPair, err := s.jwtService.GenerateTokens(user.Username, user.ID, user.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	deviceID := uuid.New().String()
	err = s.devicesRepo.CreateLoginDevice(user.ID, deviceID, tokenPair.RefreshToken, tokenPair.RefreshTokenExpiry)
	if err != nil {
		return nil, fmt.Errorf("failed to store device token: %w", err)
	}

	return &LoginResult{
		User:               user,
		AccessToken:        tokenPair.AccessToken,
		AccessTokenExpiry:  tokenPair.AccessTokenExpiry,
		RefreshToken:       tokenPair.RefreshToken,
		RefreshTokenExpiry: tokenPair.RefreshTokenExpiry,
		DeviceID:           deviceID,
	}, nil
}

// RefreshToken 刷新访问令牌
func (s *LoginService) RefreshToken(refreshToken, deviceID string) (*RefreshResult, error) {
	device, err := s.devicesRepo.GetDeviceByRefreshTokenAndDeviceID(refreshToken, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get device: %w", err)
	}
	if device == nil {
		return nil, fmt.Errorf("invalid refresh token or device ID")
	}

	user, err := s.accountsRepo.GetUserByID(device.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	if !user.IsActive() {
		return nil, fmt.Errorf("account disabled")
	}

	newRefreshToken, newRefreshTokenExpiry, err := s.jwtService.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate new refresh token: %w", err)
	}

	// 轮换刷新令牌
	err = s.devicesRepo.RotateRefreshToken(user.ID, device.DeviceID, newRefreshToken, newRefreshTokenExpiry)
	if err != nil {
		return nil, fmt.Errorf("failed to update device token: %w", err)
	}

	accessToken, accessTokenExpiry, err := s.jwtService.GenerateAccessToken(user.Username, user.ID, user.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new access token: %w", err)
	}

	return &RefreshResult{
		AccessToken:        accessToken,
		AccessTokenExpiry:  accessTokenExpiry,
		RefreshToken:       newRefreshToken,
		RefreshTokenExpiry: newRefreshTokenExpiry,
		DeviceID:           deviceID,
	}, nil
}

// Logout 执行登出操作
func (s *LoginService) Logout(deviceID string, refreshToken string) error {
	if deviceID == "" && refreshToken != "" {
		device, err := s.devicesRepo.GetDeviceByRefreshToken(refreshToken)
		if err != nil {
			return err
		}
		if device == nil {
			return errors.New("invalid refresh token")
		}
		deviceID = device.DeviceID
	}

	if deviceID != "" && refreshToken == "" {
		return s.devicesRepo.DeleteDeviceByDeviceID(deviceID)
	}

	if deviceID != "" && refreshToken != "" {
		return s.devicesRepo.DeleteDeviceByDeviceIDAndRefreshToken(deviceID, refreshToken)
	}

	return errors.New("device_id or refresh_token is required")
}

// GetDeviceExpiry 获取设备令牌的过期时间
func (s *LoginService) GetDeviceExpiry(deviceID string) (time.Time, error) {
	config := s.jwtService.GetConfig()
	return time.Now().Add(config.RefreshExpiresIn), nil
}

func (s *LoginService) Create2FAChallenge(ctx context.Context, userID uint) (ticket string, expiresIn int64, err error) {
	if s.twoFactorRepo == nil {
		return "", 0, fmt.Errorf("2FA repository not initialized")
	}

	ticket, err = generateTwoFactorTicket()
	if err != nil {
		return "", 0, err
	}
	expiresAt := time.Now().Add(twoFATicketTTL)
	challenge := &models.TwoFactorChallenge{
		TicketHash: hashTwoFactorTicket(ticket),
		UserID:     userID,
		Attempts:   0,
		ExpiresAt:  expiresAt,
	}

	if err := s.twoFactorRepo.CreateChallenge(ctx, challenge); err != nil {
		return "", 0, err
	}

	return ticket, int64(twoFATicketTTL.Seconds()), nil
}

// Verify2FA 验证 2FA 票据和 TOTP code，通过后颁发 session
func (s *LoginService) Verify2FA(ticket, code string) (*LoginResult, error) {
	if s.twoFactorRepo == nil || s.secretCrypto == nil {
		return nil, fmt.Errorf("2FA service not initialized")
	}
	ctx := context.Background()
	now := time.Now()
	ticketHash := hashTwoFactorTicket(ticket)

	var user *models.User

	err := s.twoFactorRepo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var challenge models.TwoFactorChallenge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("ticket_hash = ? AND expires_at > ? AND consumed_at IS NULL", ticketHash, now).
			First(&challenge).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidTwoFactorTicket
			}
			return err
		}

		if challenge.Attempts >= twoFAMaxAttempts {
			consumedAt := now
			if err := tx.Model(&challenge).Updates(map[string]any{"consumed_at": consumedAt}).Error; err != nil {
				return err
			}
			return ErrTooManyTwoFactorAttempt
		}

		var setting models.UserTOTPSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND enabled = ? AND secret_encrypted <> ''", challenge.UserID, true).
			First(&setting).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidTwoFactorTicket
			}
			return err
		}

		secret, err := s.secretCrypto.DecryptString(setting.SecretEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt TOTP secret: %w", err)
		}

		step, ok := mfa.ValidateTOTPCode(secret, code, now)
		if !ok {
			if err := tx.Model(&challenge).Update("attempts", challenge.Attempts+1).Error; err != nil {
				return err
			}
			return ErrInvalidTwoFactorCode
		}
		if step <= setting.LastUsedStep {
			return ErrTwoFactorReplay
		}

		if err := tx.Model(&setting).Update("last_used_step", step).Error; err != nil {
			return err
		}
		consumedAt := now
		if err := tx.Model(&challenge).Updates(map[string]any{"consumed_at": consumedAt}).Error; err != nil {
			return err
		}

		loadedUser, err := accounts.NewRepository(tx).GetUserByID(challenge.UserID)
		if err != nil {
			return err
		}
		if !loadedUser.IsActive() {
			return fmt.Errorf("account disabled")
		}
		user = loadedUser
		return nil
	})
	if err != nil {
		return nil, err
	}

	result, err := s.IssueSessionForUser(user)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func generateTwoFactorTicket() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("failed to generate 2FA ticket: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func hashTwoFactorTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}
