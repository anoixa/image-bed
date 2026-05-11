package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

const authSettingsKey = "auth:settings"

// AuthSettings contains runtime authentication switches managed from the UI.
type AuthSettings struct {
	PasswordLoginEnabled bool `json:"password_login_enabled"`
}

// GetAuthSettings returns UI-managed auth settings, falling back to static
// environment defaults when the database record has not been created yet.
func (m *Manager) GetAuthSettings(ctx context.Context, fallbackPasswordLoginEnabled bool) (*AuthSettings, error) {
	if cached, ok := m.cache.GetAuthSettings(); ok {
		return cached, nil
	}

	config, err := m.repo.GetByKey(ctx, authSettingsKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings := &AuthSettings{PasswordLoginEnabled: fallbackPasswordLoginEnabled}
			m.cache.SetAuthSettings(settings)
			return settings, nil
		}
		return nil, err
	}

	configMap, err := m.crypto.Decrypt(config.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt auth settings: %w", err)
	}

	settings := &AuthSettings{
		PasswordLoginEnabled: getBoolFromMap(configMap, "password_login_enabled", fallbackPasswordLoginEnabled),
	}
	m.cache.SetAuthSettings(settings)
	return settings, nil
}

// IsPasswordLoginEnabled returns the current password-login switch. It falls
// back to the static config on DB errors so auth endpoints remain available.
func (m *Manager) IsPasswordLoginEnabled(ctx context.Context, fallbackPasswordLoginEnabled bool) bool {
	settings, err := m.GetAuthSettings(ctx, fallbackPasswordLoginEnabled)
	if err != nil {
		configManagerLog.Warnf("Failed to load auth settings, using static fallback: %v", err)
		return fallbackPasswordLoginEnabled
	}
	return settings.PasswordLoginEnabled
}

// SetAuthSettings stores UI-managed auth settings in the encrypted config table.
func (m *Manager) SetAuthSettings(ctx context.Context, settings *AuthSettings) error {
	if settings == nil {
		return errors.New("auth settings are required")
	}

	configMap := map[string]any{
		"password_login_enabled": settings.PasswordLoginEnabled,
	}
	encryptedJSON, err := m.crypto.Encrypt(configMap)
	if err != nil {
		return fmt.Errorf("failed to encrypt auth settings: %w", err)
	}

	existing, err := m.repo.GetByKey(ctx, authSettingsKey)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		config := &models.SystemConfig{
			Category:    models.ConfigCategorySecurity,
			Name:        "Authentication Settings",
			Key:         authSettingsKey,
			ConfigJSON:  encryptedJSON,
			IsEnabled:   true,
			Description: "Runtime authentication switches such as password login availability",
		}
		if err := m.repo.Create(ctx, config); err != nil {
			return err
		}
		m.cache.Invalidate(models.ConfigCategorySecurity)
		m.cache.SetAuthSettings(settings)
		m.eventBus.Publish(EventConfigCreated, config)
		return nil
	}

	existing.ConfigJSON = encryptedJSON
	existing.IsEnabled = true
	if err := m.repo.Update(ctx, existing); err != nil {
		return err
	}
	m.cache.Invalidate(models.ConfigCategorySecurity)
	m.cache.SetAuthSettings(settings)
	m.eventBus.Publish(EventConfigUpdated, existing)
	return nil
}
