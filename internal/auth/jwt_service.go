package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	appconfig "github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/utils"

	"github.com/golang-jwt/jwt/v5"
)

// TokenPair 包含访问令牌和刷新令牌
type TokenPair struct {
	AccessToken        string
	AccessTokenExpiry  time.Time
	RefreshToken       string
	RefreshTokenExpiry time.Time
}

// TokenClaims JWT 令牌声明
type TokenClaims struct {
	Username string `json:"username"`
	UserID   uint   `json:"user_id"`
	Role     string `json:"role"`
	Type     string `json:"type"`
	jwt.RegisteredClaims
}

// JWTService JWT Token 服务 - 合并 TokenManager 功能
type JWTService struct {
	configManager *configSvc.Manager
	keysRepo      *keys.Repository
	config        TokenConfig
	mutex         sync.RWMutex
}

// TokenConfig 保存 JWT 配置
type TokenConfig struct {
	Secret           []byte
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
}

// NewJWTService 创建新的 JWT 服务
func NewJWTService(cfg *appconfig.Config, configManager *configSvc.Manager, keysRepo *keys.Repository) (*JWTService, error) {
	svc := &JWTService{
		configManager: configManager,
		keysRepo:      keysRepo,
	}

	if err := svc.initialize(cfg); err != nil {
		return nil, err
	}

	return svc, nil
}

// initialize 从应用配置初始化 JWT 配置
func (s *JWTService) initialize(cfg *appconfig.Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	return s.applyConfig(TokenConfigInput{
		Secret:          cfg.JWTSecret,
		AccessTokenTTL:  cfg.JWTAccessTokenTTL,
		RefreshTokenTTL: cfg.JWTRefreshTokenTTL,
	})
}

type TokenConfigInput struct {
	Secret          string
	AccessTokenTTL  string
	RefreshTokenTTL string
}

// applyConfig 应用 JWT 配置
func (s *JWTService) applyConfig(input TokenConfigInput) error {
	// 使用字符数而非字节长度，避免多字节UTF-8字符绕过检查
	secretRunes := []rune(input.Secret)
	if len(secretRunes) < 32 {
		return fmt.Errorf("JWT secret must be at least 32 characters long, got %d", len(secretRunes))
	}

	duration, err := time.ParseDuration(input.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid JWT access token TTL: %s", input.AccessTokenTTL)
	}

	refreshDuration, err := time.ParseDuration(input.RefreshTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid JWT refresh token TTL: %s", input.RefreshTokenTTL)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.config = TokenConfig{
		Secret:           []byte(input.Secret),
		ExpiresIn:        duration,
		RefreshExpiresIn: refreshDuration,
	}

	utils.Infof("[JWT] Config loaded from environment - Access: %v, Refresh: %v", duration, refreshDuration)
	return nil
}

// GetConfig 获取当前 JWT 配置
func (s *JWTService) GetConfig() TokenConfig {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return TokenConfig{
		Secret:           append([]byte{}, s.config.Secret...),
		ExpiresIn:        s.config.ExpiresIn,
		RefreshExpiresIn: s.config.RefreshExpiresIn,
	}
}

// SetConfig 设置 JWT 配置
func (s *JWTService) SetConfig(config TokenConfig) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.config = config
}

// GenerateTokens 生成访问令牌和刷新令牌
func (s *JWTService) GenerateTokens(username string, userID uint, role string) (*TokenPair, error) {
	config := s.GetConfig()

	if len(config.Secret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	// 生成 access token
	accessTokenExpiry := time.Now().Add(config.ExpiresIn)
	accessClaims := &TokenClaims{
		Username: username,
		UserID:   userID,
		Role:     role,
		Type:     "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessTokenExpiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(config.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// 生成 refresh token
	refreshToken, err := utils.GenerateRandomToken(64)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}
	refreshTokenExpiry := time.Now().Add(config.RefreshExpiresIn)

	return &TokenPair{
		AccessToken:        accessToken,
		AccessTokenExpiry:  accessTokenExpiry,
		RefreshToken:       refreshToken,
		RefreshTokenExpiry: refreshTokenExpiry,
	}, nil
}

// GenerateAccessToken 仅生成访问令牌
func (s *JWTService) GenerateAccessToken(username string, userID uint, role string) (string, time.Time, error) {
	config := s.GetConfig()

	if len(config.Secret) == 0 {
		return "", time.Time{}, errors.New("JWT secret is not initialized")
	}

	accessTokenExpiry := time.Now().Add(config.ExpiresIn)
	accessClaims := &TokenClaims{
		Username: username,
		UserID:   userID,
		Role:     role,
		Type:     "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessTokenExpiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(config.Secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate access token: %w", err)
	}

	return accessToken, accessTokenExpiry, nil
}

// GenerateRefreshToken 生成刷新令牌
func (s *JWTService) GenerateRefreshToken() (string, time.Time, error) {
	config := s.GetConfig()

	refreshToken, err := utils.GenerateRandomToken(64)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	refreshTokenExpiry := time.Now().Add(config.RefreshExpiresIn)
	return refreshToken, refreshTokenExpiry, nil
}

// GenerateStaticToken 生成静态令牌
func (s *JWTService) GenerateStaticToken() (string, error) {
	return utils.GenerateRandomToken(64)
}

// ParseToken 解析和验证 JWT 令牌
func (s *JWTService) ParseToken(tokenString string) (*TokenClaims, error) {
	config := s.GetConfig()

	if len(config.Secret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	claims := &TokenClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return config.Secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// ExtractClaims 从令牌中提取声明
func (s *JWTService) ExtractClaims(tokenString string) (*TokenClaims, error) {
	return s.ParseToken(tokenString)
}

// ValidateToken 验证令牌是否有效
func (s *JWTService) ValidateToken(tokenString string) error {
	_, err := s.ParseToken(tokenString)
	return err
}

// IsAccessToken 检查令牌是否为访问令牌
func (s *JWTService) IsAccessToken(tokenString string) (bool, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return false, err
	}

	return claims.Type == "access", nil
}

// ValidateStaticToken 验证静态令牌
func (s *JWTService) ValidateStaticToken(ctx context.Context, token string) (*StaticTokenUser, error) {
	// 检查 API Key 是否启用
	if s.configManager != nil {
		settings, err := s.configManager.GetImageProcessingSettings(ctx)
		if err == nil && !settings.APIKeyEnabled {
			return nil, errors.New("API key authentication is disabled")
		}
	}

	if s.keysRepo == nil {
		return nil, errors.New("keys repository not initialized")
	}

	user, err := s.keysRepo.GetUserByApiToken(token)
	if err != nil {
		return nil, err
	}

	return &StaticTokenUser{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, nil
}

// StaticTokenUser 静态令牌用户信息
type StaticTokenUser struct {
	ID       uint
	Username string
	Role     string
}
