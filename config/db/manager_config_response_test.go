package config

import (
	"context"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCreateConfigReturnsMaskedSensitiveValues(t *testing.T) {
	manager := newTestManager(t)

	resp, err := manager.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Name:     "s3-private",
		Config: map[string]any{
			"type":              "s3",
			"access_key_id":     "AKIA_TEST",
			"secret_access_key": "secret-value",
			"client_secret":     "oauth-secret",
			"bucket_name":       "images",
		},
	}, 7)
	require.NoError(t, err)

	assert.Equal(t, "******", resp.Config["access_key_id"])
	assert.Equal(t, "******", resp.Config["secret_access_key"])
	assert.Equal(t, "******", resp.Config["client_secret"])
	assert.Equal(t, "images", resp.Config["bucket_name"])

	stored, err := manager.repo.GetByID(context.Background(), resp.ID)
	require.NoError(t, err)
	assert.NotContains(t, stored.ConfigJSON, "secret-value")

	unmasked, err := manager.ToResponseWithMask(context.Background(), stored, false)
	require.NoError(t, err)
	assert.Equal(t, "secret-value", unmasked.Config["secret_access_key"])
}

func TestUpdateConfigReturnsMaskedSensitiveValues(t *testing.T) {
	manager := newTestManager(t)

	created, err := manager.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Name:     "webdav-private",
		Config: map[string]any{
			"type":            "webdav",
			"webdav_url":      "https://dav.example.com",
			"webdav_password": "old-secret",
		},
	}, 7)
	require.NoError(t, err)

	updated, err := manager.UpdateConfig(context.Background(), created.ID, &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Name:     "webdav-private",
		Config: map[string]any{
			"webdav_password": "new-secret",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "******", updated.Config["webdav_password"])

	stored, err := manager.repo.GetByID(context.Background(), updated.ID)
	require.NoError(t, err)
	unmasked, err := manager.ToResponseWithMask(context.Background(), stored, false)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", unmasked.Config["webdav_password"])
}

func TestOAuthConfigStoredMaskedAndLoaded(t *testing.T) {
	manager := newTestManager(t)

	resp, err := manager.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryOAuth,
		Name:     "GitHub",
		Config: map[string]any{
			"provider":      "github",
			"client_id":     "client-id",
			"client_secret": "client-secret",
		},
	}, 7)
	require.NoError(t, err)
	assert.Equal(t, "******", resp.Config["client_secret"])

	providers, total, err := manager.GetOAuthProviderConfigs(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, providers, 1)
	assert.Equal(t, "github", providers[0].Provider)
	assert.Equal(t, "client-id", providers[0].ClientID)
	assert.Equal(t, "client-secret", providers[0].ClientSecret)
}

func TestOAuthConfigRejectsInvalidProvider(t *testing.T) {
	manager := newTestManager(t)

	_, err := manager.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryOAuth,
		Name:     "Invalid",
		Config: map[string]any{
			"provider":      "unknown",
			"client_id":     "client-id",
			"client_secret": "client-secret",
		},
	}, 7)
	assert.ErrorIs(t, err, ErrInvalidOAuthConfig)
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemConfig{}))

	manager := NewManager(db, t.TempDir())
	require.NoError(t, manager.crypto.Initialize())
	return manager
}
