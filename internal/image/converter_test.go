package image

import (
	"context"
	"io"
	"testing"
	"time"

	configdb "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testStorageProvider struct{}

func (p *testStorageProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *testStorageProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	return nil, nil
}

func (p *testStorageProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *testStorageProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return false, nil
}

func (p *testStorageProvider) Health(ctx context.Context) error {
	return nil
}

func (p *testStorageProvider) Name() string {
	return "test"
}

func TestShouldStartVariantPipeline(t *testing.T) {
	assert.True(t, shouldStartVariantPipeline(true, false, false))
	assert.True(t, shouldStartVariantPipeline(false, true, false))
	assert.True(t, shouldStartVariantPipeline(false, false, true))
	assert.True(t, shouldStartVariantPipeline(true, true, false))
	assert.True(t, shouldStartVariantPipeline(true, false, true))
	assert.True(t, shouldStartVariantPipeline(false, true, true))
	assert.True(t, shouldStartVariantPipeline(true, true, true))
	assert.False(t, shouldStartVariantPipeline(false, false, false))
}

func TestShouldTriggerVariantConversion(t *testing.T) {
	settings := &configdb.ImageProcessingSettings{
		ThumbnailEnabled:         true,
		ThumbnailSizes:           models.DefaultThumbnailSizes,
		ConversionEnabledFormats: []string{models.FormatWebP},
		SkipSmallerThan:          10,
	}

	t.Run("eligible image triggers conversion", func(t *testing.T) {
		image := &models.Image{MimeType: "image/jpeg", FileSize: 32 * 1024}
		assert.True(t, shouldTriggerVariantConversion(image, settings))
	})

	t.Run("gif does not trigger conversion", func(t *testing.T) {
		image := &models.Image{MimeType: "image/gif", FileSize: 32 * 1024}
		assert.False(t, shouldTriggerVariantConversion(image, settings))
	})

	t.Run("small image does not trigger conversion", func(t *testing.T) {
		image := &models.Image{MimeType: "image/jpeg", FileSize: 5 * 1024}
		assert.False(t, shouldTriggerVariantConversion(image, settings))
	})

	t.Run("all formats disabled does not trigger conversion", func(t *testing.T) {
		image := &models.Image{MimeType: "image/jpeg", FileSize: 32 * 1024}
		disabled := &configdb.ImageProcessingSettings{}
		assert.False(t, shouldTriggerVariantConversion(image, disabled))
	})
}

func TestVariantReadyForSubmit(t *testing.T) {
	now := time.Now()

	t.Run("pending variant without retry window is ready", func(t *testing.T) {
		variant := &models.ImageVariant{Status: models.VariantStatusPending}
		assert.True(t, variantReadyForSubmit(variant, now, false))
	})

	t.Run("pending variant with future retry window is not ready", func(t *testing.T) {
		retryAt := now.Add(5 * time.Minute)
		variant := &models.ImageVariant{
			Status:      models.VariantStatusPending,
			NextRetryAt: &retryAt,
		}
		assert.False(t, variantReadyForSubmit(variant, now, false))
		assert.True(t, variantReadyForSubmit(variant, now, true))
	})

	t.Run("failed variant is never ready", func(t *testing.T) {
		variant := &models.ImageVariant{Status: models.VariantStatusFailed}
		assert.False(t, variantReadyForSubmit(variant, now, false))
	})
}

func TestGetStorageForImageDoesNotFallbackForMissingSpecificProvider(t *testing.T) {
	converter := &Converter{storage: &testStorageProvider{}}
	image := &models.Image{StorageConfigID: 999999}

	assert.Nil(t, converter.getStorageForImage(image))
}

func TestFailPendingVariantsOnSubmitFailureMarksVariantsAndImageFailed(t *testing.T) {
	db := setupConverterTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)

	image := &models.Image{
		Identifier:      "img-1",
		OriginalName:    "test.jpg",
		FileHash:        "hash-1",
		FileSize:        1024,
		MimeType:        "image/jpeg",
		StoragePath:     "original/test.jpg",
		StorageConfigID: 1,
		UserID:          1,
		VariantStatus:   models.ImageVariantStatusNone,
	}
	require.NoError(t, imageRepo.SaveImage(image))

	thumb := &models.ImageVariant{ImageID: image.ID, Format: models.FormatThumbnailSize(600), Status: models.VariantStatusPending}
	webp := &models.ImageVariant{ImageID: image.ID, Format: models.FormatWebP, Status: models.VariantStatusPending}
	require.NoError(t, db.Create(thumb).Error)
	require.NoError(t, db.Create(webp).Error)

	converter := &Converter{
		imageRepo:   imageRepo,
		variantRepo: variantRepo,
	}

	converter.failPendingVariantsOnSubmitFailure(image, "worker task submission rejected", thumb, webp)

	updatedImage, err := imageRepo.GetImageByIdentifier(image.Identifier)
	require.NoError(t, err)
	assert.Equal(t, models.ImageVariantStatusFailed, updatedImage.VariantStatus)

	updatedThumb, err := variantRepo.GetByID(thumb.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusFailed, updatedThumb.Status)
	assert.Contains(t, updatedThumb.ErrorMessage, "worker task submission rejected")

	updatedWebP, err := variantRepo.GetByID(webp.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusFailed, updatedWebP.Status)
	assert.Contains(t, updatedWebP.ErrorMessage, "worker task submission rejected")
}

func setupConverterTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&models.Image{}, &models.ImageVariant{}))
	return db
}
