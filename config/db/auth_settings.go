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

	settings, err := parseAuthSettings(configMap)
	if err != nil {
		return nil, err
	}
	m.cache.SetAuthSettings(settings)
	return settings, nil
}

// parseAuthSettings strictly decodes a decrypted auth-settings payload.
//
// password_login_enabled must be present and a real boolean. We intentionally
// do NOT reuse the lenient getBoolFromMap here: a missing key, null, or a
// wrong-typed value indicates a corrupted or tampered record, and silently
// coercing it to a default would re-open password login on a security control
// that an administrator may have deliberately disabled. Returning an error lets
// IsPasswordLoginEnabled fail closed.
func parseAuthSettings(configMap map[string]any) (*AuthSettings, error) {
	raw, ok := configMap["password_login_enabled"]
	if !ok {
		return nil, errors.New("auth settings: missing password_login_enabled")
	}
	enabled, ok := raw.(bool)
	if !ok {
		return nil, fmt.Errorf("auth settings: password_login_enabled must be a bool, got %T", raw)
	}
	return &AuthSettings{PasswordLoginEnabled: enabled}, nil
}

// IsPasswordLoginEnabled returns the current password-login switch.
//
// GetAuthSettings already returns the static default for a missing record
// (first-run, never configured). Any error reaching here is therefore an
// operational failure — DB read, decryption or parsing — and we fail closed by
// disabling password login. Treating such failures as "use the static default"
// would let a transient config fault silently re-enable password login that an
// administrator deliberately turned off (e.g. OAuth-only deployments).
func (m *Manager) IsPasswordLoginEnabled(ctx context.Context, fallbackPasswordLoginEnabled bool) bool {
	settings, err := m.GetAuthSettings(ctx, fallbackPasswordLoginEnabled)
	if err != nil {
		configManagerLog.Errorf("Failed to load auth settings, failing closed (password login disabled): %v", err)
		return false
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
