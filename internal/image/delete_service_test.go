package image

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupDeleteServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Image{}, &models.ImageVariant{}))
	return db
}

func TestDeleteSingleKeepsSharedOriginalFile(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, storage.InitStorage([]storage.StorageConfig{{
		ID:        1,
		Name:      "local",
		Type:      "local",
		IsDefault: true,
		LocalPath: tempDir,
	}}))

	storagePath := "original/shared.jpg"
	fullPath := filepath.Join(tempDir, storagePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte("image-bytes"), 0600))

	db := setupDeleteServiceTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)
	service := NewDeleteService(imageRepo, variantRepo, cache.NewHelper(nil))

	imageOne := &models.Image{
		Identifier:      "img-1",
		OriginalName:    "one.jpg",
		FileHash:        "hash-1",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StoragePath:     storagePath,
		StorageConfigID: 1,
		UserID:          1,
	}
	imageTwo := &models.Image{
		Identifier:      "img-2",
		OriginalName:    "two.jpg",
		FileHash:        "hash-2",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StoragePath:     storagePath,
		StorageConfigID: 1,
		UserID:          2,
	}
	require.NoError(t, imageRepo.SaveImage(imageOne))
	require.NoError(t, imageRepo.SaveImage(imageTwo))

	result, err := service.DeleteSingle(context.Background(), imageOne.Identifier, imageOne.UserID)
	require.NoError(t, err)
	require.True(t, result.Success)

	_, statErr := os.Stat(fullPath)
	assert.NoError(t, statErr, "shared original file must not be deleted while another image still references it")

	remaining, err := imageRepo.GetImageByIdentifier(imageTwo.Identifier)
	require.NoError(t, err)
	assert.Equal(t, imageTwo.Identifier, remaining.Identifier)
}

func TestDeleteSingleCancelsProcessingVariantsAndDeletesCompletedVariants(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, storage.InitStorage([]storage.StorageConfig{{
		ID:        1,
		Name:      "local",
		Type:      "local",
		IsDefault: true,
		LocalPath: tempDir,
	}}))

	originalPath := "original/delete-me.jpg"
	completedVariantPath := "converted/delete-me.webp"
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "original"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "converted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, originalPath), []byte("image-bytes"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, completedVariantPath), []byte("webp-bytes"), 0o600))

	db := setupDeleteServiceTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)
	service := NewDeleteService(imageRepo, variantRepo, cache.NewHelper(nil))

	img := &models.Image{
		Identifier:      "delete-me",
		OriginalName:    "delete-me.jpg",
		FileHash:        "hash-delete-me",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StoragePath:     originalPath,
		StorageConfigID: 1,
		UserID:          1,
		VariantStatus:   models.ImageVariantStatusProcessing,
	}
	require.NoError(t, imageRepo.SaveImage(img))

	completedVariant := &models.ImageVariant{
		ImageID:     img.ID,
		Format:      models.FormatWebP,
		Status:      models.VariantStatusCompleted,
		Identifier:  "delete-me.webp",
		StoragePath: completedVariantPath,
		FileHash:    "hash-webp",
		FileSize:    10,
	}
	processingVariant := &models.ImageVariant{
		ImageID:     img.ID,
		Format:      models.FormatAVIF,
		Status:      models.VariantStatusProcessing,
		Identifier:  "",
		StoragePath: "",
		FileHash:    "",
		FileSize:    0,
	}
	require.NoError(t, db.Create(completedVariant).Error)
	require.NoError(t, db.Create(processingVariant).Error)

	result, err := service.DeleteSingle(context.Background(), img.Identifier, img.UserID)
	require.NoError(t, err)
	require.True(t, result.Success)

	_, err = variantRepo.GetByID(completedVariant.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, statErr := os.Stat(filepath.Join(tempDir, completedVariantPath))
	assert.True(t, os.IsNotExist(statErr), "completed variant file should be deleted")

	canceledVariant, err := variantRepo.GetByID(processingVariant.ID)
	require.NoError(t, err)
	assert.Equal(t, models.VariantStatusCanceled, canceledVariant.Status)
	assert.Contains(t, canceledVariant.ErrorMessage, "image deleted")
}

func TestDeleteSingleKeepsSharedVariantFile(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, storage.InitStorage([]storage.StorageConfig{{
		ID:        1,
		Name:      "local",
		Type:      "local",
		IsDefault: true,
		LocalPath: tempDir,
	}}))

	originalPath := "original/shared.jpg"
	variantPath := "converted/shared.webp"
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "original"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "converted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, originalPath), []byte("image-bytes"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, variantPath), []byte("webp-bytes"), 0o600))

	db := setupDeleteServiceTestDB(t)
	imageRepo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)
	service := NewDeleteService(imageRepo, variantRepo, cache.NewHelper(nil))

	imageOne := &models.Image{
		Identifier:      "shared-1",
		OriginalName:    "one.jpg",
		FileHash:        "hash-shared-1",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StoragePath:     originalPath,
		StorageConfigID: 1,
		UserID:          1,
		VariantStatus:   models.ImageVariantStatusCompleted,
	}
	imageTwo := &models.Image{
		Identifier:      "shared-2",
		OriginalName:    "two.jpg",
		FileHash:        "hash-shared-2",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StoragePath:     originalPath,
		StorageConfigID: 1,
		UserID:          2,
		VariantStatus:   models.ImageVariantStatusCompleted,
	}
	require.NoError(t, imageRepo.SaveImage(imageOne))
	require.NoError(t, imageRepo.SaveImage(imageTwo))

	variantOne := &models.ImageVariant{
		ImageID:     imageOne.ID,
		Format:      models.FormatWebP,
		Status:      models.VariantStatusCompleted,
		Identifier:  "shared-1.webp",
		StoragePath: variantPath,
		FileHash:    "hash-webp-1",
		FileSize:    10,
	}
	variantTwo := &models.ImageVariant{
		ImageID:     imageTwo.ID,
		Format:      models.FormatWebP,
		Status:      models.VariantStatusCompleted,
		Identifier:  "shared-2.webp",
		StoragePath: variantPath,
		FileHash:    "hash-webp-2",
		FileSize:    10,
	}
	require.NoError(t, db.Create(variantOne).Error)
	require.NoError(t, db.Create(variantTwo).Error)

	result, err := service.DeleteSingle(context.Background(), imageOne.Identifier, imageOne.UserID)
	require.NoError(t, err)
	require.True(t, result.Success)

	_, err = variantRepo.GetByID(variantOne.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	remainingVariant, err := variantRepo.GetByID(variantTwo.ID)
	require.NoError(t, err)
	assert.Equal(t, variantPath, remainingVariant.StoragePath)

	_, statErr := os.Stat(filepath.Join(tempDir, variantPath))
	assert.NoError(t, statErr, "shared variant file must not be deleted while another image still references it")
}
