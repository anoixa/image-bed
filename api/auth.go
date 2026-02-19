package api

import (
	"time"

	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/services/auth"
	configSvc "github.com/anoixa/image-bed/config/db"
)

var (
	// jwtService JWT 服务实例
	jwtService *auth.JWTService
	tokenManager *auth.TokenManager
	authKeysRepo *keys.Repository
)

// SetJWTService 设置 JWT 服务
func SetJWTService(service *auth.JWTService) {
	jwtService = service
}

// SetTokenManager 设置 Token 管理器
func SetTokenManager(manager *auth.TokenManager) {
	tokenManager = manager
}

// TokenInitFromManager 从配置管理器初始化 JWT
func TokenInitFromManager(manager *configSvc.Manager) error {
	var err error
	tokenManager, err = auth.NewTokenManager(manager)
	if err != nil {
		return err
	}
	jwtService = auth.NewJWTService(tokenManager, authKeysRepo)
	return nil
}

// GetJWTConfig 获取当前 JWT 配置（只读）
func GetJWTConfig() (secret []byte, expiresIn, refreshExpiresIn time.Duration) {
	if tokenManager == nil {
		return nil, 0, 0
	}
	config := tokenManager.GetConfig()
	return config.Secret, config.ExpiresIn, config.RefreshExpiresIn
}

// GetJWTService 获取 JWT 服务实例
func GetJWTService() *auth.JWTService {
	return jwtService
}

// InitTestJWT 初始化测试用的 JWT 配置
func InitTestJWT(secret, expiresInStr, refreshExpiresInStr string) error {
	expiresIn, err := time.ParseDuration(expiresInStr)
	if err != nil {
		return err
	}
	refreshExpiresIn, err := time.ParseDuration(refreshExpiresInStr)
	if err != nil {
		return err
	}

	tokenManager = &auth.TokenManager{}
	tokenManager.SetConfig(auth.TokenConfig{
		Secret:           []byte(secret),
		ExpiresIn:        expiresIn,
		RefreshExpiresIn: refreshExpiresIn,
	})
	jwtService = auth.NewJWTService(tokenManager, authKeysRepo)
	return nil
}
