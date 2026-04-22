package cmd

import (
	"testing"

	"github.com/anoixa/image-bed/api/core"
	"github.com/anoixa/image-bed/database/models"
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

func setupServeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Image{}, &models.ImageVariant{}))
	return db
}
