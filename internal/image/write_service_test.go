package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
	0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

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

func newTestWriteService(t *testing.T, db *gorm.DB) (*WriteService, *repoimages.Repository, *repoimages.VariantRepository) {
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

	service := NewWriteService(repo, nil, nil, helper, "http://localhost:8080")
	return service, repo, variantRepo
}

func TestCreateDedupedImageRecordPreservesOriginalStorageConfig(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo, _ := newTestWriteService(t, db)

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

	deduped, err := service.createDedupedImageRecord(context.Background(), existing, 2, "copy.webp", 99, false)
	require.NoError(t, err)

	assert.Equal(t, existing.StoragePath, deduped.StoragePath)
	assert.Equal(t, existing.StorageConfigID, deduped.StorageConfigID)
	assert.Equal(t, existing.FileHash, deduped.FileHash)
	assert.Equal(t, uint(2), deduped.UserID)
	assert.NotEqual(t, existing.Identifier, deduped.Identifier)
}

func TestDeleteSingleDoesNotDeletePhysicalFileWhenDatabaseDeleteFails(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo, variantRepo := newTestWriteService(t, db)
	deleteService := NewDeleteService(repo, variantRepo, service.cacheHelper)

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
			addErr := tx.AddError(errors.New("forced image delete failure"))
			require.Error(t, addErr)
			assert.Contains(t, addErr.Error(), "forced image delete failure")
		}
	}))

	result, err := deleteService.DeleteSingle(context.Background(), image.Identifier, image.UserID)
	require.Error(t, err)
	assert.Nil(t, result)

	_, statErr := os.Stat(absolutePath)
	assert.NoError(t, statErr, "physical file should remain when metadata deletion fails")

	stored, getErr := repo.GetImageByIdentifier(image.Identifier)
	require.NoError(t, getErr)
	assert.Equal(t, image.Identifier, stored.Identifier)
}

func TestUploadSingleSourceDoesNotReuseSoftDeletedImageWithoutBackingFile(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo, _ := newTestWriteService(t, db)

	const providerID uint = 91002
	tempDir := t.TempDir()
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID:        providerID,
		Name:      "test-local-reuse",
		Type:      "local",
		LocalPath: tempDir,
	}))
	t.Cleanup(func() {
		_ = storage.RemoveProvider(providerID)
	})

	fileHashBytes := sha256.Sum256(tinyPNG)
	fileHash := hex.EncodeToString(fileHashBytes[:])

	softDeleted := &models.Image{
		Identifier:      "deleted-image",
		StoragePath:     "original/old/deleted.png",
		OriginalName:    "deleted.png",
		FileSize:        int64(len(tinyPNG)),
		MimeType:        "image/png",
		StorageConfigID: providerID,
		FileHash:        fileHash,
		Width:           1,
		Height:          1,
		UserID:          1,
		IsPublic:        true,
	}
	require.NoError(t, repo.SaveImage(softDeleted))
	require.NoError(t, repo.DeleteImage(softDeleted))

	uploadPath := filepath.Join(t.TempDir(), "upload.png")
	require.NoError(t, os.WriteFile(uploadPath, tinyPNG, 0o644))

	result, err := service.UploadSingleSource(
		context.Background(),
		1,
		NewTempUploadSource("upload.png", uploadPath, int64(len(tinyPNG))),
		providerID,
		true,
		0,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsDuplicate)
	assert.NotNil(t, result.Image)
	assert.NotEqual(t, softDeleted.Identifier, result.Image.Identifier)
	assert.NotEqual(t, softDeleted.StoragePath, result.Image.StoragePath)

	restored, err := repo.GetSoftDeletedImageByHash(fileHash)
	require.NoError(t, err)
	assert.Equal(t, softDeleted.Identifier, restored.Identifier)
	assert.NotNil(t, restored.DeletedAt)

	active, err := repo.GetImageByHash(fileHash)
	require.NoError(t, err)
	assert.Equal(t, result.Image.Identifier, active.Identifier)

	_, statErr := os.Stat(filepath.Join(tempDir, result.Image.StoragePath))
	assert.NoError(t, statErr)
}

func TestUploadSingleSourceCleansTempFileForDuplicateImage(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo, _ := newTestWriteService(t, db)

	const providerID uint = 91003
	tempDir := t.TempDir()
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID:        providerID,
		Name:      "test-local-duplicate-cleanup",
		Type:      "local",
		LocalPath: tempDir,
	}))
	t.Cleanup(func() {
		_ = storage.RemoveProvider(providerID)
	})

	fileHashBytes := sha256.Sum256(tinyPNG)
	fileHash := hex.EncodeToString(fileHashBytes[:])
	existing := &models.Image{
		Identifier:      "dup-image",
		StoragePath:     "original/2026/04/dup.png",
		OriginalName:    "dup.png",
		FileSize:        int64(len(tinyPNG)),
		MimeType:        "image/png",
		StorageConfigID: providerID,
		FileHash:        fileHash,
		Width:           1,
		Height:          1,
		UserID:          1,
		IsPublic:        true,
	}
	require.NoError(t, repo.SaveImage(existing))

	uploadPath := filepath.Join(t.TempDir(), "duplicate-upload.png")
	require.NoError(t, os.WriteFile(uploadPath, tinyPNG, 0o644))

	result, err := service.UploadSingleSource(
		context.Background(),
		1,
		NewTempUploadSource("duplicate-upload.png", uploadPath, int64(len(tinyPNG))),
		providerID,
		true,
		0,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsDuplicate)

	_, statErr := os.Stat(uploadPath)
	require.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}

func TestUploadSingleSourceCleansTempFileForReusableSoftDeletedImage(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, repo, _ := newTestWriteService(t, db)

	const providerID uint = 91004
	tempDir := t.TempDir()
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID:        providerID,
		Name:      "test-local-soft-delete-cleanup",
		Type:      "local",
		LocalPath: tempDir,
	}))
	t.Cleanup(func() {
		_ = storage.RemoveProvider(providerID)
	})

	fileHashBytes := sha256.Sum256(tinyPNG)
	fileHash := hex.EncodeToString(fileHashBytes[:])

	storagePath := "original/2026/04/reusable.png"
	absolutePath := filepath.Join(tempDir, storagePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), 0o755))
	require.NoError(t, os.WriteFile(absolutePath, tinyPNG, 0o644))

	softDeleted := &models.Image{
		Identifier:      "soft-deleted-reusable",
		StoragePath:     storagePath,
		OriginalName:    "reusable.png",
		FileSize:        int64(len(tinyPNG)),
		MimeType:        "image/png",
		StorageConfigID: providerID,
		FileHash:        fileHash,
		Width:           1,
		Height:          1,
		UserID:          1,
		IsPublic:        true,
	}
	require.NoError(t, repo.SaveImage(softDeleted))
	require.NoError(t, repo.DeleteImage(softDeleted))

	uploadPath := filepath.Join(t.TempDir(), "reusable-upload.png")
	require.NoError(t, os.WriteFile(uploadPath, tinyPNG, 0o644))

	result, err := service.UploadSingleSource(
		context.Background(),
		2,
		NewTempUploadSource("reusable-upload.png", uploadPath, int64(len(tinyPNG))),
		providerID,
		true,
		0,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsDuplicate)
	assert.NotEqual(t, softDeleted.Identifier, result.Identifier)
	assert.Equal(t, uint(2), result.Image.UserID)

	_, statErr := os.Stat(uploadPath)
	require.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}

func TestUploadSingleSourceCleansTempFileWhenConversionTaskIsDropped(t *testing.T) {
	db := setupImageServiceTestDB(t)
	service, _, _ := newTestWriteService(t, db)
	service.converter = &Converter{}

	const providerID uint = 91005
	tempDir := t.TempDir()
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID:        providerID,
		Name:      "test-local-task-drop-cleanup",
		Type:      "local",
		LocalPath: tempDir,
	}))
	t.Cleanup(func() {
		_ = storage.RemoveProvider(providerID)
	})

	uploadPath := filepath.Join(t.TempDir(), "dropped-task-upload.png")
	require.NoError(t, os.WriteFile(uploadPath, tinyPNG, 0o644))

	result, err := service.UploadSingleSource(
		context.Background(),
		1,
		NewTempUploadSource("dropped-task-upload.png", uploadPath, int64(len(tinyPNG))),
		providerID,
		true,
		0,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsDuplicate)

	_, statErr := os.Stat(uploadPath)
	require.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}
