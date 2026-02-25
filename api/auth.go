package api

import (
	"time"

	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/auth"
)

var (
	// jwtService JWT 服务实例
	jwtService   *auth.JWTService
	authKeysRepo *keys.Repository
)

// SetJWTService 设置 JWT 服务
func SetJWTService(service *auth.JWTService) {
	jwtService = service
}

// SetAuthKeysRepo 设置 Keys 仓库
func SetAuthKeysRepo(repo *keys.Repository) {
	authKeysRepo = repo
}

// TokenInitFromManager 从配置管理器初始化 JWT
func TokenInitFromManager(manager *configSvc.Manager) error {
	var err error
	jwtService, err = auth.NewJWTService(manager, authKeysRepo)
	return err
}

// GetJWTConfig 获取当前 JWT 配置
func GetJWTConfig() (secret []byte, expiresIn, refreshExpiresIn time.Duration) {
	if jwtService == nil {
		return nil, 0, 0
	}
	config := jwtService.GetConfig()
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

	jwtService = &auth.JWTService{}
	jwtService.SetConfig(auth.TokenConfig{
		Secret:           []byte(secret),
		ExpiresIn:        expiresIn,
		RefreshExpiresIn: refreshExpiresIn,
	})
	return nil
}
