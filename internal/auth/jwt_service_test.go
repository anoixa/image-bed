package auth

import (
	"testing"

	appconfig "github.com/anoixa/image-bed/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJWTServiceLoadsConfigFromEnvConfig(t *testing.T) {
	cfg := &appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "30m",
		JWTRefreshTokenTTL: "168h",
	}

	svc, err := NewJWTService(cfg, nil, nil)
	require.NoError(t, err)

	tokenCfg := svc.GetConfig()
	assert.Equal(t, []byte(cfg.JWTSecret), tokenCfg.Secret)
	assert.Equal(t, "30m0s", tokenCfg.ExpiresIn.String())
	assert.Equal(t, "168h0m0s", tokenCfg.RefreshExpiresIn.String())
}

func TestNewJWTServiceRejectsShortSecret(t *testing.T) {
	cfg := &appconfig.Config{
		JWTSecret:          "too-short",
		JWTAccessTokenTTL:  "30m",
		JWTRefreshTokenTTL: "168h",
	}

	_, err := NewJWTService(cfg, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT secret must be at least 32 characters long")
}
