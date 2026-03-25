package image

import (
	"context"
	"errors"
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

func setupImageServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&models.User{}, &models.Album{}, &models.Image{}, &models.ImageVariant{})
	require.NoError(t, err)

	return db
}

func newTestImageService(t *testing.T, db *gorm.DB) (*Service, *repoimages.Repository) {
	t.Helper()

	provider, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1_000,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = provider.Close()
	})

	repo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)
	helper := cache.NewHelper(provider)

	service := NewService(repo, variantRepo, nil, nil, nil, nil, helper, nil, "http://localhost:8080")
	return service, repo
}

func TestCreateDedupedImageRecordPreservesOriginalStorageConfig(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo := newTestImageService(t, db)

	existing := &models.Image{
		Identifier:      "origin-image",
		StoragePath:     "2026/03/origin.webp",
		OriginalName:    "origin.webp",
		FileSize:        1024,
		MimeType:        "image/webp",
		StorageConfigID: 7,
		FileHash:        "hash-dedup-preserve-storage",
		UserID:          1,
		IsPublic:        true,
	}
	require.NoError(t, repo.SaveImage(existing))

	deduped, err := service.createDedupedImageRecord(existing, 2, "copy.webp", 99, false)
	require.NoError(t, err)

	assert.Equal(t, existing.StoragePath, deduped.StoragePath)
	assert.Equal(t, existing.StorageConfigID, deduped.StorageConfigID)
	assert.Equal(t, existing.FileHash, deduped.FileHash)
	assert.Equal(t, uint(2), deduped.UserID)
	assert.NotEqual(t, existing.Identifier, deduped.Identifier)
}

func TestDeleteSingleDoesNotDeletePhysicalFileWhenDatabaseDeleteFails(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo := newTestImageService(t, db)

	const providerID uint = 91001
	tempDir := t.TempDir()
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID:        providerID,
		Name:      "test-local",
		Type:      "local",
		LocalPath: tempDir,
	}))
	t.Cleanup(func() {
		_ = storage.RemoveProvider(providerID)
	})

	storagePath := "2026/03/delete-test.jpg"
	absolutePath := filepath.Join(tempDir, storagePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), 0o755))
	require.NoError(t, os.WriteFile(absolutePath, []byte("image-bytes"), 0o644))

	image := &models.Image{
		Identifier:      "delete-failure-image",
		StoragePath:     storagePath,
		OriginalName:    "delete.jpg",
		FileSize:        int64(len("image-bytes")),
		MimeType:        "image/jpeg",
		StorageConfigID: providerID,
		FileHash:        "hash-delete-single-db-failure",
		UserID:          1,
		IsPublic:        true,
	}
	require.NoError(t, repo.SaveImage(image))

	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:fail_image_delete", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "images" {
			tx.AddError(errors.New("forced image delete failure"))
		}
	}))

	result, err := service.DeleteSingle(context.Background(), image.Identifier, image.UserID)
	require.Error(t, err)
	assert.Nil(t, result)

	_, statErr := os.Stat(absolutePath)
	assert.NoError(t, statErr, "physical file should remain when metadata deletion fails")

	stored, getErr := repo.GetImageByIdentifier(image.Identifier)
	require.NoError(t, getErr)
	assert.Equal(t, image.Identifier, stored.Identifier)
}
