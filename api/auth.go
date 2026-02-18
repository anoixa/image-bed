package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/app"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/utils"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret           []byte
	jwtExpiresIn        time.Duration
	jwtRefreshExpiresIn time.Duration
	authKeysRepo        *keys.Repository

	// jwtConfigManager 配置管理器
	jwtConfigManager *configSvc.Manager
	jwtConfigMutex   sync.RWMutex
)

// SetAuthRepositories 认证相关
func SetAuthRepositories(container *app.Container) {
	authKeysRepo = container.KeysRepo
}

// TokenInitFromManager 从配置管理器初始化 JWT
func TokenInitFromManager(manager *configSvc.Manager) error {
	jwtConfigManager = manager

	// 获取 JWT 配置
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jwtConfig, err := manager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config from database: %w", err)
	}

	// 应用配置
	if err := applyJWTConfig(jwtConfig); err != nil {
		return err
	}

	// 订阅配置变更事件（热重载）
	manager.Subscribe(configSvc.EventConfigUpdated, func(event *configSvc.Event) {
		if event.Config.Category == models.ConfigCategoryJWT {
			log.Println("[JWT] Configuration updated, reloading...")
			if err := reloadJWTConfig(); err != nil {
				log.Printf("[JWT] Failed to reload config: %v", err)
			} else {
				log.Println("[JWT] Configuration reloaded successfully")
			}
		}
	})

	return nil
}

// TokenInit 初始化 JWT 配置（传统方式，用于向后兼容）
// Deprecated: 使用 TokenInitFromManager 代替
func TokenInit(secret, expiresIn, refreshExpiresIn string) error {
	if len(secret) < 32 {
		return fmt.Errorf("JWT secret must be at least 32 characters long, got %d", len(secret))
	}

	jwtConfigMutex.Lock()
	defer jwtConfigMutex.Unlock()

	jwtSecret = []byte(secret)

	duration, err := time.ParseDuration(expiresIn)
	if err != nil {
		return fmt.Errorf("invalid JWT expiration duration: %s", expiresIn)
	}
	jwtExpiresIn = duration

	refreshDuration, err := time.ParseDuration(refreshExpiresIn)
	if err != nil {
		return fmt.Errorf("invalid JWT refresh expiration duration: %s", refreshExpiresIn)
	}
	jwtRefreshExpiresIn = refreshDuration

	log.Printf("JWT Config loaded - Access: %v, Refresh: %v\n", jwtExpiresIn, jwtRefreshExpiresIn)

	return nil
}

// applyJWTConfig 应用 JWT 配置
func applyJWTConfig(jwtConfig *configSvc.JWTConfig) error {
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

	jwtConfigMutex.Lock()
	defer jwtConfigMutex.Unlock()

	jwtSecret = []byte(jwtConfig.Secret)
	jwtExpiresIn = duration
	jwtRefreshExpiresIn = refreshDuration

	log.Printf("[JWT] Config loaded from database - Access: %v, Refresh: %v\n", jwtExpiresIn, jwtRefreshExpiresIn)
	return nil
}

// reloadJWTConfig 重新加载 JWT 配置（热重载）
func reloadJWTConfig() error {
	if jwtConfigManager == nil {
		return errors.New("config manager not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jwtConfig, err := jwtConfigManager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config: %w", err)
	}

	return applyJWTConfig(jwtConfig)
}

// GetJWTConfig 获取当前 JWT 配置（只读）
func GetJWTConfig() (secret []byte, expiresIn, refreshExpiresIn time.Duration) {
	jwtConfigMutex.RLock()
	defer jwtConfigMutex.RUnlock()
	return jwtSecret, jwtExpiresIn, jwtRefreshExpiresIn
}

// GenerateTokens Generate access token and refresh token
func GenerateTokens(username string, userID uint, role string) (accessToken string, accessTokenExpiry time.Time, err error) {
	jwtConfigMutex.RLock()
	secret := jwtSecret
	expiresIn := jwtExpiresIn
	jwtConfigMutex.RUnlock()

	if len(secret) == 0 {
		err = errors.New("JWT secret is not initialized")
		return
	}

	// 生成access token (JWT)
	accessTokenExpiry = time.Now().Add(expiresIn)
	accessClaims := jwt.MapClaims{
		"username": username,
		"user_id":  userID,
		"role":     role,
		"type":     "access",
		"exp":      accessTokenExpiry.Unix(),
		"iat":      time.Now().Unix(),
	}

	accessToken, err = jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(secret)
	if err != nil {
		err = fmt.Errorf("failed to generate access token: %w", err)
		// 重置返回值
		accessToken = ""
		accessTokenExpiry = time.Time{}
		return
	}

	return
}

// GenerateRefreshToken Generate refresh token
func GenerateRefreshToken() (refreshToken string, refreshTokenExpiry time.Time, err error) {
	jwtConfigMutex.RLock()
	refreshExpiresIn := jwtRefreshExpiresIn
	jwtConfigMutex.RUnlock()

	refreshToken, err = utils.GenerateRandomToken(64)

	refreshTokenExpiry = time.Now().Add(refreshExpiresIn)
	return
}

// GenerateStaticToken Generate a new static key
func GenerateStaticToken() (refreshToken string, err error) {
	refreshToken, err = utils.GenerateRandomToken(64)

	return
}

// Parse Parse and validate JWT token
func Parse(tokenString string) (jwt.MapClaims, error) {
	jwtConfigMutex.RLock()
	secret := jwtSecret
	jwtConfigMutex.RUnlock()

	if len(secret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	// 解析令牌
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
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

// ValidateStaticToken Verify static token
func ValidateStaticToken(tokenString string) (*models.User, error) {
	if authKeysRepo == nil {
		return nil, errors.New("auth repositories not initialized")
	}
	user, err := authKeysRepo.GetUserByApiToken(tokenString)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid static token")
	}
	return user, nil
}
