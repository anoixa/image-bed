package images

import (
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVariantStatusConstants(t *testing.T) {
	assert.Equal(t, "pending", models.VariantStatusPending)
	assert.Equal(t, "processing", models.VariantStatusProcessing)
	assert.Equal(t, "completed", models.VariantStatusCompleted)
	assert.Equal(t, "failed", models.VariantStatusFailed)
}

func TestFormatConstants(t *testing.T) {
	assert.Equal(t, "webp", models.FormatWebP)
	assert.Equal(t, "avif", models.FormatAVIF)
}

func TestRecoverStaleProcessing(t *testing.T) {
	db := setupVariantRepoTestDB(t)
	repo := NewVariantRepository(db)

	staleTime := time.Now().Add(-20 * time.Minute)
	retrySoon := time.Now().Add(-30 * time.Minute)

	resetVariant := &models.ImageVariant{
		ImageID:      1,
		Format:       models.FormatWebP,
		Status:       models.VariantStatusProcessing,
		RetryCount:   0,
		NextRetryAt:  &retrySoon,
		Identifier:   "reset.webp",
		StoragePath:  "converted/reset.webp",
		FileHash:     "hash-reset",
		FileSize:     1,
		CreatedAt:    staleTime,
		UpdatedAt:    staleTime,
		ErrorMessage: "old error",
	}
	failedVariant := &models.ImageVariant{
		ImageID:      2,
		Format:       models.FormatThumbnailSize(600),
		Status:       models.VariantStatusProcessing,
		RetryCount:   2,
		Identifier:   "failed.webp",
		StoragePath:  "thumbnails/failed.webp",
		FileHash:     "hash-failed",
		FileSize:     1,
		CreatedAt:    staleTime,
		UpdatedAt:    staleTime,
		ErrorMessage: "old error",
	}

	require.NoError(t, db.Create(resetVariant).Error)
	require.NoError(t, db.Create(failedVariant).Error)

	resetCount, failedCount, err := repo.RecoverStaleProcessing(15*time.Minute, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resetCount)
	assert.Equal(t, int64(1), failedCount)

	updatedReset, err := repo.GetByID(resetVariant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusPending, updatedReset.Status)
	assert.Equal(t, 1, updatedReset.RetryCount)
	assert.Empty(t, updatedReset.ErrorMessage)
	require.NotNil(t, updatedReset.NextRetryAt)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), *updatedReset.NextRetryAt, 5*time.Second)

	updatedFailed, err := repo.GetByID(failedVariant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusFailed, updatedFailed.Status)
	assert.Equal(t, 3, updatedFailed.RetryCount)
	assert.Contains(t, updatedFailed.ErrorMessage, "retry limit")
	assert.Nil(t, updatedFailed.NextRetryAt)
}

func TestUpdateCompletedClearsRetryMetadata(t *testing.T) {
	db := setupVariantRepoTestDB(t)
	repo := NewVariantRepository(db)

	retryAt := time.Now().Add(10 * time.Minute)
	variant := &models.ImageVariant{
		ImageID:     1,
		Format:      models.FormatWebP,
		Status:      models.VariantStatusProcessing,
		RetryCount:  2,
		NextRetryAt: &retryAt,
		Identifier:  "before.webp",
		StoragePath: "converted/before.webp",
		FileHash:    "hash-before",
		FileSize:    1,
	}
	require.NoError(t, db.Create(variant).Error)

	err := repo.UpdateCompleted(variant.ID, "after.webp", "converted/after.webp", 12, "hash-after", 100, 200)
	require.NoError(t, err)

	updated, err := repo.GetByID(variant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusCompleted, updated.Status)
	assert.Equal(t, 0, updated.RetryCount)
	assert.Nil(t, updated.NextRetryAt)
}

func TestResetVariantsToPendingClearsRetryWindow(t *testing.T) {
	db := setupVariantRepoTestDB(t)
	repo := NewVariantRepository(db)

	retryAt := time.Now().Add(30 * time.Minute)
	variant := &models.ImageVariant{
		ImageID:      1,
		Format:       models.FormatWebP,
		Status:       models.VariantStatusProcessing,
		RetryCount:   2,
		NextRetryAt:  &retryAt,
		ErrorMessage: "stuck",
	}
	require.NoError(t, db.Create(variant).Error)

	rows, err := repo.ResetVariantsToPending([]uint{variant.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	updated, err := repo.GetByID(variant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusPending, updated.Status)
	assert.Empty(t, updated.ErrorMessage)
	assert.Nil(t, updated.NextRetryAt)
	assert.Equal(t, 2, updated.RetryCount)
}

func setupVariantRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ImageVariant{}))
	return db
}
