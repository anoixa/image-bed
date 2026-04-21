package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSweepOnceRetriggeredImagesAreNotResetByBulkImageUpdates(t *testing.T) {
	db := setupSweeperTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)

	staleTime := time.Now().Add(-20 * time.Minute)
	image := &models.Image{
		Identifier:    "sweeper-retry",
		OriginalName:  "retry.jpg",
		FileHash:      "sweeper-retry-hash",
		StoragePath:   "original/retry.jpg",
		MimeType:      "image/jpeg",
		UserID:        1,
		VariantStatus: models.ImageVariantStatusProcessing,
		CreatedAt:     staleTime,
		UpdatedAt:     staleTime,
	}
	require.NoError(t, imageRepo.SaveImage(image))

	variant := &models.ImageVariant{
		ImageID:    image.ID,
		Format:     models.FormatWebP,
		Status:     models.VariantStatusProcessing,
		CreatedAt:  staleTime,
		UpdatedAt:  staleTime,
		Identifier: "retry.webp",
	}
	require.NoError(t, db.Create(variant).Error)

	var triggerCalls atomic.Int32
	sweepOnce(context.Background(), variantRepo, imageRepo, func(img *models.Image) {
		require.Equal(t, image.ID, img.ID)
		triggerCalls.Add(1)
	})

	updatedVariant, err := variantRepo.GetByID(variant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusPending, updatedVariant.Status)
	assert.Equal(t, 1, updatedVariant.RetryCount)
	require.NotNil(t, updatedVariant.NextRetryAt)

	updatedImage, err := imageRepo.GetImageByID(image.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ImageVariantStatusProcessing, updatedImage.VariantStatus)
	assert.Equal(t, int32(1), triggerCalls.Load())
}

func TestSweepOnceMarksImagesFailedWhenRecoveredVariantHitsRetryLimit(t *testing.T) {
	db := setupSweeperTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)

	staleTime := time.Now().Add(-20 * time.Minute)
	image := &models.Image{
		Identifier:    "sweeper-fail",
		OriginalName:  "fail.jpg",
		FileHash:      "sweeper-fail-hash",
		StoragePath:   "original/fail.jpg",
		MimeType:      "image/jpeg",
		UserID:        1,
		VariantStatus: models.ImageVariantStatusProcessing,
		CreatedAt:     staleTime,
		UpdatedAt:     staleTime,
	}
	require.NoError(t, imageRepo.SaveImage(image))

	variant := &models.ImageVariant{
		ImageID:      image.ID,
		Format:       models.FormatWebP,
		Status:       models.VariantStatusProcessing,
		RetryCount:   staleMaxRetries - 1,
		CreatedAt:    staleTime,
		UpdatedAt:    staleTime,
		Identifier:   "fail.webp",
		ErrorMessage: "old error",
	}
	require.NoError(t, db.Create(variant).Error)

	var triggerCalls atomic.Int32
	sweepOnce(context.Background(), variantRepo, imageRepo, func(*models.Image) {
		triggerCalls.Add(1)
	})

	updatedVariant, err := variantRepo.GetByID(variant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusFailed, updatedVariant.Status)
	assert.Equal(t, staleMaxRetries, updatedVariant.RetryCount)
	assert.Contains(t, updatedVariant.ErrorMessage, "retry limit")
	assert.Nil(t, updatedVariant.NextRetryAt)

	updatedImage, err := imageRepo.GetImageByID(image.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ImageVariantStatusFailed, updatedImage.VariantStatus)
	assert.Equal(t, int32(0), triggerCalls.Load())
}

func setupSweeperTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Image{}, &models.ImageVariant{}))
	return db
}
