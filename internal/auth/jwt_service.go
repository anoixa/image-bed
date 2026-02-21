package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/utils"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService JWT Token 服务
type JWTService struct {
	tokenManager *TokenManager
	keysRepo     *keys.Repository
}

// TokenPair 包含访问令牌和刷新令牌
type TokenPair struct {
	AccessToken       string
	AccessTokenExpiry time.Time
	RefreshToken      string
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

// NewJWTService 创建新的 JWT 服务
func NewJWTService(tokenManager *TokenManager, keysRepo *keys.Repository) *JWTService {
	return &JWTService{
		tokenManager: tokenManager,
		keysRepo:     keysRepo,
	}
}

// GenerateTokens 生成访问令牌和刷新令牌
func (s *JWTService) GenerateTokens(username string, userID uint, role string) (*TokenPair, error) {
	config := s.tokenManager.GetConfig()

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
	config := s.tokenManager.GetConfig()

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
	config := s.tokenManager.GetConfig()

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
	config := s.tokenManager.GetConfig()

	if len(config.Secret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	// 解析令牌
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return config.Secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// 验证令牌有效性
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// ValidateStaticToken 验证静态令牌
func (s *JWTService) ValidateStaticToken(tokenString string) (*models.User, error) {
	if s.keysRepo == nil {
		return nil, errors.New("auth repositories not initialized")
	}
	user, err := s.keysRepo.GetUserByApiToken(tokenString)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid static token")
	}
	return user, nil
}
