package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/api/core"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResetVariantWorkSnapshotsResetsProcessingState(t *testing.T) {
	db := setupServeTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)

	image := &models.Image{
		Identifier:    "shutdown-image",
		OriginalName:  "shutdown.jpg",
		FileHash:      "shutdown-hash",
		StoragePath:   "original/shutdown.jpg",
		UserID:        1,
		VariantStatus: models.ImageVariantStatusProcessing,
	}
	require.NoError(t, imageRepo.SaveImage(image))

	thumb := &models.ImageVariant{
		ImageID: image.ID,
		Format:  models.FormatThumbnailSize(600),
		Status:  models.VariantStatusProcessing,
	}
	webp := &models.ImageVariant{
		ImageID: image.ID,
		Format:  models.FormatWebP,
		Status:  models.VariantStatusCompleted,
	}
	require.NoError(t, db.Create(thumb).Error)
	require.NoError(t, db.Create(webp).Error)

	deps := &Dependencies{
		DB:          db,
		VariantRepo: variantRepo,
		Repositories: &core.Repositories{
			ImagesRepo: imageRepo,
		},
	}

	err := resetVariantWorkSnapshots(deps, []worker.InFlightTaskSnapshot{
		{
			ImageID:    image.ID,
			VariantIDs: []uint{thumb.ID, webp.ID, thumb.ID},
		},
	})
	require.NoError(t, err)

	updatedThumb, err := variantRepo.GetByID(thumb.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusPending, updatedThumb.Status)
	assert.Nil(t, updatedThumb.NextRetryAt)
	assert.Empty(t, updatedThumb.ErrorMessage)

	updatedWebP, err := variantRepo.GetByID(webp.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusCompleted, updatedWebP.Status)

	updatedImage, err := imageRepo.GetImageByID(image.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ImageVariantStatusNone, updatedImage.VariantStatus)
}

func TestResetVariantWorkSnapshotsNoopWhenEmpty(t *testing.T) {
	deps := &Dependencies{}
	require.NoError(t, resetVariantWorkSnapshots(deps, nil))
}

func TestInitDatabaseBackfillsLegacyUserStatusAndCreatesActiveAdmin(t *testing.T) {
	db := setupServeTestDB(t)
	accountsRepo := accounts.NewRepository(db)

	require.NoError(t, db.Exec(
		`INSERT INTO users (username, password, role, status, created_at, updated_at) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"legacy-user", "hash", models.RoleUser, "",
	).Error)

	deps := &Dependencies{
		DB: db,
		Repositories: &core.Repositories{
			AccountsRepo: accountsRepo,
		},
	}

	require.NoError(t, InitDatabase(deps))

	legacyUser, err := accountsRepo.GetUserByUsername("legacy-user")
	require.NoError(t, err)
	assert.Equal(t, models.UserStatusActive, legacyUser.Status)

	adminUser, err := accountsRepo.GetUserByUsername("admin")
	require.NoError(t, err)
	assert.Equal(t, models.RoleAdmin, adminUser.Role)
	assert.Equal(t, models.UserStatusActive, adminUser.Status)
}

func TestEnsureJWTSecretKeepsConfiguredSecret(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{JWTSecret: "configured-secret-at-least-32-chars"}

	require.NoError(t, ensureJWTSecret(cfg, dataDir))

	assert.Equal(t, "configured-secret-at-least-32-chars", cfg.JWTSecret)
	assert.NoFileExists(t, filepath.Join(dataDir, jwtSecretFileName))
}

func TestEnsureJWTSecretGeneratesPersistentSecret(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{}

	require.NoError(t, ensureJWTSecret(cfg, dataDir))
	assert.Len(t, []rune(cfg.JWTSecret), 43)

	secretPath := filepath.Join(dataDir, jwtSecretFileName)
	stat, err := os.Stat(secretPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())

	secondCfg := &config.Config{}
	require.NoError(t, ensureJWTSecret(secondCfg, dataDir))
	assert.Equal(t, cfg.JWTSecret, secondCfg.JWTSecret)
}

func TestEnsureJWTSecretRejectsInvalidStoredSecret(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, jwtSecretFileName), []byte("short"), 0o600))

	err := ensureJWTSecret(&config.Config{}, dataDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func setupServeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Image{}, &models.ImageVariant{}))
	return db
}
