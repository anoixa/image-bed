package api

import (
	"time"

	appconfig "github.com/anoixa/image-bed/config"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/auth"
)

// NewJWTServiceFromConfig 从应用配置创建 JWT 服务
func NewJWTServiceFromConfig(cfg *appconfig.Config, manager *configSvc.Manager, keysRepo *keys.Repository) (*auth.JWTService, error) {
	return auth.NewJWTService(cfg, manager, keysRepo)
}

// NewTestJWTService 创建测试用 JWT 服务
func NewTestJWTService(secret, expiresInStr, refreshExpiresInStr string) (*auth.JWTService, error) {
	expiresIn, err := time.ParseDuration(expiresInStr)
	if err != nil {
		return nil, err
	}
	refreshExpiresIn, err := time.ParseDuration(refreshExpiresInStr)
	if err != nil {
		return nil, err
	}

	jwtService := &auth.JWTService{}
	jwtService.SetConfig(auth.TokenConfig{
		Secret:           []byte(secret),
		ExpiresIn:        expiresIn,
		RefreshExpiresIn: refreshExpiresIn,
	})
	return jwtService, nil
}
