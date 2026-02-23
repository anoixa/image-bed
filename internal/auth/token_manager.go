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
)

// TokenConfig 保存 JWT 配置
type TokenConfig struct {
	Secret           []byte
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
}

// TokenManager Token 配置管理器
type TokenManager struct {
	configManager *configSvc.Manager
	config        TokenConfig
	mutex         sync.RWMutex
}

// NewTokenManager 创建新的 Token 管理器
func NewTokenManager(manager *configSvc.Manager) (*TokenManager, error) {
	tm := &TokenManager{
		configManager: manager,
	}

	// 从配置管理器初始化
	if err := tm.InitializeFromManager(); err != nil {
		return nil, err
	}

	return tm, nil
}

// InitializeFromManager 从配置管理器初始化 JWT 配置
func (tm *TokenManager) InitializeFromManager() error {
	if tm.configManager == nil {
		return errors.New("config manager is nil")
	}

	// 获取 JWT 配置
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jwtConfig, err := tm.configManager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config from database: %w", err)
	}

	// 应用配置
	if err := tm.ApplyConfig(jwtConfig); err != nil {
		return err
	}

	// 订阅配置变更事件（热重载）
	tm.configManager.Subscribe(configSvc.EventConfigUpdated, func(event *configSvc.Event) {
		if event.Config.Category == models.ConfigCategoryJWT {
			log.Println("[JWT] Configuration updated, reloading...")
			if err := tm.ReloadConfig(); err != nil {
				log.Printf("[JWT] Failed to reload config: %v", err)
			} else {
				log.Println("[JWT] Configuration reloaded successfully")
			}
		}
	})

	return nil
}

// ApplyConfig 应用 JWT 配置
func (tm *TokenManager) ApplyConfig(jwtConfig *configSvc.JWTConfig) error {
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

	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tm.config = TokenConfig{
		Secret:           []byte(jwtConfig.Secret),
		ExpiresIn:        duration,
		RefreshExpiresIn: refreshDuration,
	}

	log.Printf("[JWT] Config loaded from database - Access: %v, Refresh: %v\n", duration, refreshDuration)
	return nil
}

// ReloadConfig 重新加载 JWT 配置（热重载）
func (tm *TokenManager) ReloadConfig() error {
	if tm.configManager == nil {
		return errors.New("config manager not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jwtConfig, err := tm.configManager.GetJWTConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JWT config: %w", err)
	}

	return tm.ApplyConfig(jwtConfig)
}

// GetConfig 获取当前 JWT 配置（只读）
func (tm *TokenManager) GetConfig() TokenConfig {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	return TokenConfig{
		Secret:           append([]byte{}, tm.config.Secret...),
		ExpiresIn:        tm.config.ExpiresIn,
		RefreshExpiresIn: tm.config.RefreshExpiresIn,
	}
}

// SetConfig 设置 JWT 配置（仅用于测试）
func (tm *TokenManager) SetConfig(config TokenConfig) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.config = config
}
