package auth

import (
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/google/uuid"
)

// LoginResult 登录结果
type LoginResult struct {
	User               *models.User
	AccessToken        string
	AccessTokenExpiry  time.Time
	RefreshToken       string
	RefreshTokenExpiry time.Time
	DeviceID           string
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
	accountsRepo *accounts.Repository
	devicesRepo  *accounts.DeviceRepository
	jwtService   *JWTService
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

// ValidateCredentials 验证用户凭据
func (s *LoginService) ValidateCredentials(username, password string) (*models.User, bool, error) {
	user, err := s.accountsRepo.GetUserByUsername(username)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		return nil, false, nil
	}

	ok, err := cryptopackage.ComparePasswordAndHash(password, user.Password)
	if err != nil {
		return nil, false, fmt.Errorf("password comparison failed: %w", err)
	}

	return user, ok, nil
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
func (s *LoginService) Logout(deviceID string) error {
	return s.devicesRepo.DeleteDeviceByDeviceID(deviceID)
}

// GetDeviceExpiry 获取设备令牌的过期时间
func (s *LoginService) GetDeviceExpiry(deviceID string) (time.Time, error) {
	config := s.jwtService.tokenManager.GetConfig()
	return time.Now().Add(config.RefreshExpiresIn), nil
}
