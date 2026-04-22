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
