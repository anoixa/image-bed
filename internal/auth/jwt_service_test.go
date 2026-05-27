package auth

import (
	"context"
	"testing"

	appconfig "github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func TestValidateAccessTokenClaimsUsesCurrentUserStatusAndRole(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))

	repo := accounts.NewRepository(db)
	user := &models.User{
		Username: "current-user",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(user))

	cfg := &appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "30m",
		JWTRefreshTokenTTL: "168h",
	}
	svc, err := NewJWTService(cfg, nil, nil, repo)
	require.NoError(t, err)

	token, _, err := svc.GenerateAccessToken("old-name", user.ID, models.RoleAdmin)
	require.NoError(t, err)
	claims, err := svc.ParseToken(token)
	require.NoError(t, err)

	authUser, err := svc.ValidateAccessTokenClaims(context.Background(), claims)
	require.NoError(t, err)
	assert.Equal(t, "current-user", authUser.Username)
	assert.Equal(t, models.RoleUser, authUser.Role)

	require.NoError(t, repo.UpdateUserStatus(user.ID, models.UserStatusDisabled))
	_, err = svc.ValidateAccessTokenClaims(context.Background(), claims)
	require.Error(t, err)
}
