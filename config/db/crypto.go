package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/configs"
	cryptoservice "github.com/anoixa/image-bed/internal/crypto"
	"gorm.io/gorm"
)

// CryptoLayer 配置加密层
type CryptoLayer struct {
	repo   configs.Repository
	crypto *cryptoservice.Service
}

// NewCryptoLayer 创建加密层
func NewCryptoLayer(repo configs.Repository, crypto *cryptoservice.Service) *CryptoLayer {
	return &CryptoLayer{
		repo:   repo,
		crypto: crypto,
	}
}

// Initialize 初始化加密
func (c *CryptoLayer) Initialize() error {
	checkDataExists := func() (bool, error) {
		count, err := c.repo.Count(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "no such table") ||
				strings.Contains(err.Error(), "relation") && strings.Contains(err.Error(), "does not exist") ||
				errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		return count > 0, nil
	}

	if err := c.crypto.Initialize(checkDataExists); err != nil {
		return fmt.Errorf("failed to initialize crypto service: %w", err)
	}

	if err := c.ensureCanary(); err != nil {
		return fmt.Errorf("failed to ensure canary: %w", err)
	}

	return nil
}

// Encrypt 加密配置
func (c *CryptoLayer) Encrypt(config map[string]interface{}) (string, error) {
	jsonData, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}
	return c.crypto.EncryptString(string(jsonData)), nil
}

// Decrypt 解密配置
func (c *CryptoLayer) Decrypt(encrypted string) (map[string]interface{}, error) {
	return c.crypto.DecryptJSON(encrypted)
}

// ensureCanary 验证/创建 Canary
func (c *CryptoLayer) ensureCanary() error {
	ctx := context.Background()

	canary, err := c.repo.GetByKey(ctx, "system:encryption_canary")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) ||
			strings.Contains(err.Error(), "no such table") ||
			strings.Contains(err.Error(), "relation") && strings.Contains(err.Error(), "does not exist") {
			return c.createCanary(ctx)
		}
		return err
	}

	_, err = c.crypto.DecryptString(canary.ConfigJSON)
	if err != nil {
		return fmt.Errorf("failed to decrypt canary, master key may be incorrect: %w", err)
	}

	log.Println("[CryptoLayer] Canary verified successfully")
	return nil
}

// createCanary 创建 Canary 记录
func (c *CryptoLayer) createCanary(ctx context.Context) error {
	canaryData := map[string]string{
		"check":       "ok",
		"version":     "1",
		"description": "Encryption verification canary",
	}

	jsonData, _ := json.Marshal(canaryData)
	encrypted := c.crypto.EncryptString(string(jsonData))

	canary := &models.SystemConfig{
		Category:    models.ConfigCategorySystem,
		Name:        "Encryption Canary",
		Key:         "system:encryption_canary",
		IsEnabled:   true,
		IsDefault:   false,
		ConfigJSON:  encrypted,
		Description: "Internal configuration used to verify the correctness of encryption keys",
	}

	if err := c.repo.Create(ctx, canary); err != nil {
		return err
	}

	log.Println("[CryptoLayer] Canary created successfully")
	return nil
}
