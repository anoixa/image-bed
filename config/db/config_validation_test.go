package config

import (
	"context"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validImageProcessingConfigMap() map[string]any {
	settings := DefaultImageProcessingSettings()
	return map[string]any{
		"thumbnail_enabled":          settings.ThumbnailEnabled,
		"thumbnail_sizes":            settings.ThumbnailSizes,
		"thumbnail_quality":          settings.ThumbnailQuality,
		"conversion_enabled_formats": settings.ConversionEnabledFormats,
		"webp_quality":               settings.WebPQuality,
		"webp_effort":                settings.WebPEffort,
		"avif_quality":               settings.AVIFQuality,
		"avif_speed":                 settings.AVIFSpeed,
		"avif_experimental":          settings.AVIFExperimental,
		"skip_smaller_than":          settings.SkipSmallerThan,
		"max_dimension":              settings.MaxDimension,
		"default_album_id":           settings.DefaultAlbumID,
		"default_visibility":         settings.DefaultVisibility,
		"concurrent_upload_limit":    settings.ConcurrentUploadLimit,
		"max_file_size_mb":           settings.MaxFileSizeMB,
		"max_batch_total_mb":         settings.MaxBatchTotalMB,
		"api_key_enabled":            settings.APIKeyEnabled,
	}
}

func TestValidateSystemConfigMapStorageStrict(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name: "unknown key",
			config: map[string]any{
				"type":       "local",
				"local_path": t.TempDir(),
				"surprise":   true,
			},
		},
		{
			name: "wrong bool type",
			config: map[string]any{
				"type":              "s3",
				"endpoint":          "https://s3.example.com",
				"bucket_name":       "images",
				"access_key_id":     "access",
				"secret_access_key": "secret",
				"force_path_style":  "true",
			},
		},
		{
			name: "missing required field",
			config: map[string]any{
				"type":              "s3",
				"endpoint":          "https://s3.example.com",
				"bucket_name":       "images",
				"secret_access_key": "secret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSystemConfigMap(models.ConfigCategoryStorage, tt.config)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestValidateSystemConfigMapOAuthStrict(t *testing.T) {
	err := ValidateSystemConfigMap(models.ConfigCategoryOAuth, map[string]any{
		"provider":      "github",
		"client_id":     123,
		"client_secret": "secret",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOAuthConfig)

	err = ValidateSystemConfigMap(models.ConfigCategoryOAuth, map[string]any{
		"provider":      "github",
		"client_id":     "client",
		"client_secret": "secret",
		"extra":         "nope",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOAuthConfig)
}

func TestValidateSystemConfigMapSecurityStrict(t *testing.T) {
	err := ValidateSystemConfigMap(models.ConfigCategorySecurity, map[string]any{
		"password_login_enabled": "true",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	err = ValidateSystemConfigMap(models.ConfigCategorySecurity, map[string]any{
		"password_login_enabled": true,
		"unknown":                false,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateSystemConfigMapImageProcessingStrict(t *testing.T) {
	valid := validImageProcessingConfigMap()
	require.NoError(t, ValidateSystemConfigMap(models.ConfigCategoryImageProcessing, valid))

	wrongType := validImageProcessingConfigMap()
	wrongType["thumbnail_enabled"] = "true"
	err := ValidateSystemConfigMap(models.ConfigCategoryImageProcessing, wrongType)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	unknown := validImageProcessingConfigMap()
	unknown["mystery"] = 1
	err = ValidateSystemConfigMap(models.ConfigCategoryImageProcessing, unknown)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	jsonLike := validImageProcessingConfigMap()
	jsonLike["thumbnail_sizes"] = []any{
		map[string]any{"name": "default", "width": float64(600), "height": float64(0)},
	}
	require.NoError(t, ValidateSystemConfigMap(models.ConfigCategoryImageProcessing, jsonLike))

	outOfRange := validImageProcessingConfigMap()
	outOfRange["thumbnail_quality"] = float64(uint64(1) << 63)
	err = ValidateSystemConfigMap(models.ConfigCategoryImageProcessing, outOfRange)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestUpdateConfigRejectsWrongTypedMergedConfigAndPreservesStoredValue(t *testing.T) {
	manager := newTestManager(t)

	created, err := manager.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Name:     "s3",
		Config: map[string]any{
			"type":              "s3",
			"endpoint":          "https://s3.example.com",
			"bucket_name":       "images",
			"access_key_id":     "access",
			"secret_access_key": "secret",
			"force_path_style":  true,
		},
	}, 0)
	require.NoError(t, err)

	_, err = manager.UpdateConfig(context.Background(), created.ID, &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryStorage,
		Config: map[string]any{
			"force_path_style": "true",
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	stored, err := manager.GetConfig(context.Background(), created.ID, false)
	require.NoError(t, err)
	assert.Equal(t, true, stored.Config["force_path_style"])
}
