package config

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anoixa/image-bed/database/models"
)

// ErrInvalidOAuthConfig indicates an invalid OAuth provider configuration.
var ErrInvalidOAuthConfig = errors.New("invalid oauth config")

// OAuthProviderConfig is a decrypted OAuth provider configuration loaded from DB.
type OAuthProviderConfig struct {
	Provider     string
	ClientID     string
	ClientSecret string
}

// GetOAuthProviderConfigs returns enabled OAuth provider configs and the total
// number of DB OAuth configs. The total is used to decide whether env fallback
// should be considered.
func (m *Manager) GetOAuthProviderConfigs(ctx context.Context) ([]OAuthProviderConfig, int, error) {
	configs, err := m.repo.List(ctx, models.ConfigCategoryOAuth, false)
	if err != nil {
		return nil, 0, err
	}

	result := make([]OAuthProviderConfig, 0, len(configs))
	for _, cfg := range configs {
		if !cfg.IsEnabled {
			continue
		}

		configMap, err := m.crypto.Decrypt(cfg.ConfigJSON)
		if err != nil {
			return nil, len(configs), fmt.Errorf("failed to decrypt OAuth config ID=%d: %w", cfg.ID, err)
		}

		provider, err := parseOAuthProviderConfig(configMap)
		if err != nil {
			return nil, len(configs), fmt.Errorf("invalid OAuth config ID=%d: %w", cfg.ID, err)
		}
		result = append(result, provider)
	}

	return result, len(configs), nil
}

func ValidateOAuthConfigMap(configMap map[string]any) error {
	_, err := parseOAuthProviderConfig(configMap)
	return err
}

func parseOAuthProviderConfig(configMap map[string]any) (OAuthProviderConfig, error) {
	if err := rejectUnknownKeys(configMap, keySet("provider", "client_id", "client_secret")); err != nil {
		return OAuthProviderConfig{}, fmt.Errorf("%w: %v", ErrInvalidOAuthConfig, err)
	}

	providerRaw, err := requiredString(configMap, "provider")
	if err != nil {
		return OAuthProviderConfig{}, fmt.Errorf("%w: %v", ErrInvalidOAuthConfig, err)
	}
	provider := strings.ToLower(strings.TrimSpace(providerRaw))
	switch provider {
	case "github", "google", "gitee":
	default:
		return OAuthProviderConfig{}, fmt.Errorf("%w: provider must be one of github, google, gitee", ErrInvalidOAuthConfig)
	}

	clientID, err := requiredString(configMap, "client_id")
	if err != nil {
		return OAuthProviderConfig{}, fmt.Errorf("%w: %v", ErrInvalidOAuthConfig, err)
	}
	clientID = strings.TrimSpace(clientID)

	clientSecret, err := requiredString(configMap, "client_secret")
	if err != nil {
		return OAuthProviderConfig{}, fmt.Errorf("%w: %v", ErrInvalidOAuthConfig, err)
	}
	clientSecret = strings.TrimSpace(clientSecret)
	if clientSecret == "" || clientSecret == "******" {
		return OAuthProviderConfig{}, fmt.Errorf("%w: client_secret is required", ErrInvalidOAuthConfig)
	}

	return OAuthProviderConfig{
		Provider:     provider,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}
