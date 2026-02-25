package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
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
	Username string
	UserID   uint
	Role     string
	Type     string
	Exp      int64
	Iat      int64
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
func NewJWTService(configManager *configSvc.Manager, keysRepo *keys.Repository) (*JWTService, error) {
	svc := &JWTService{
		configManager: configManager,
		keysRepo:      keysRepo,
	}

	if err := svc.initialize(); err != nil {
		return nil, err
	}

	return svc, nil
}

// initialize 从配置管理器初始化 JWT 配置
func (s *JWTService) initialize() error {
	if s.configManager == nil {
		return errors.New("config manager is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jwtConfig, err := s.configManager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config from database: %w", err)
	}

	if err := s.applyConfig(jwtConfig); err != nil {
		return err
	}

	s.configManager.Subscribe(configSvc.EventConfigUpdated, func(event *configSvc.Event) {
		if event.Config.Category == models.ConfigCategoryJWT {
			log.Println("[JWT] Configuration updated, reloading...")
			if err := s.reloadConfig(); err != nil {
				log.Printf("[JWT] Failed to reload config: %v", err)
			} else {
				log.Println("[JWT] Configuration reloaded successfully")
			}
		}
	})

	return nil
}

// applyConfig 应用 JWT 配置
func (s *JWTService) applyConfig(jwtConfig *configSvc.JWTConfig) error {
	if len(jwtConfig.Secret) < 32 {
		return fmt.Errorf("JWT secret must be at least 32 characters long, got %d", len(jwtConfig.Secret))
	}

	duration, err := time.ParseDuration(jwtConfig.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid JWT access token TTL: %s", jwtConfig.AccessTokenTTL)
	}

	refreshDuration, err := time.ParseDuration(jwtConfig.RefreshTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid JWT refresh token TTL: %s", jwtConfig.RefreshTokenTTL)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.config = TokenConfig{
		Secret:           []byte(jwtConfig.Secret),
		ExpiresIn:        duration,
		RefreshExpiresIn: refreshDuration,
	}

	log.Printf("[JWT] Config loaded from database - Access: %v, Refresh: %v\n", duration, refreshDuration)
	return nil
}

// reloadConfig 重新加载 JWT 配置（热重载）
func (s *JWTService) reloadConfig() error {
	if s.configManager == nil {
		return errors.New("config manager not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jwtConfig, err := s.configManager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config: %w", err)
	}

	return s.applyConfig(jwtConfig)
}

// GetConfig 获取当前 JWT 配置（只读）
func (s *JWTService) GetConfig() TokenConfig {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return TokenConfig{
		Secret:           append([]byte{}, s.config.Secret...),
		ExpiresIn:        s.config.ExpiresIn,
		RefreshExpiresIn: s.config.RefreshExpiresIn,
	}
}

// SetConfig 设置 JWT 配置（仅用于测试）
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
	accessClaims := jwt.MapClaims{
		"username": username,
		"user_id":  userID,
		"role":     role,
		"type":     "access",
		"exp":      accessTokenExpiry.Unix(),
		"iat":      time.Now().Unix(),
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
	accessClaims := jwt.MapClaims{
		"username": username,
		"user_id":  userID,
		"role":     role,
		"type":     "access",
		"exp":      accessTokenExpiry.Unix(),
		"iat":      time.Now().Unix(),
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
func (s *JWTService) ParseToken(tokenString string) (jwt.MapClaims, error) {
	config := s.GetConfig()

	if len(config.Secret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return config.Secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// ExtractClaims 从令牌中提取声明
func (s *JWTService) ExtractClaims(tokenString string) (*TokenClaims, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}

	username, _ := claims["username"].(string)
	role, _ := claims["role"].(string)
	tokenType, _ := claims["type"].(string)

	userIDFloat, _ := claims["user_id"].(float64)
	userID := uint(userIDFloat)

	expFloat, _ := claims["exp"].(float64)
	iatFloat, _ := claims["iat"].(float64)

	return &TokenClaims{
		Username: username,
		UserID:   userID,
		Role:     role,
		Type:     tokenType,
		Exp:      int64(expFloat),
		Iat:      int64(iatFloat),
	}, nil
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

	tokenType, _ := claims["type"].(string)
	return tokenType == "access", nil
}

// ValidateStaticToken 验证静态令牌
func (s *JWTService) ValidateStaticToken(token string) (*StaticTokenUser, error) {
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
