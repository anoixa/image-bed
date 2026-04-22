package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBackupTableIncludesImageVariants(t *testing.T) {
	db := setupBackupRestoreTestDB(t)

	image := &models.Image{
		Identifier:      "backup-image",
		StoragePath:     "original/backup.png",
		OriginalName:    "backup.png",
		FileSize:        123,
		MimeType:        "image/png",
		StorageConfigID: 1,
		FileHash:        "backup-hash",
		UserID:          1,
	}
	require.NoError(t, db.Create(image).Error)

	nextRetryAt := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	variant := &models.ImageVariant{
		ImageID:      image.ID,
		Format:       models.FormatWebP,
		Identifier:   "backup-image.webp",
		StoragePath:  "converted/webp/backup-image.webp",
		FileSize:     64,
		FileHash:     "variant-hash",
		Width:        100,
		Height:       100,
		Status:       models.VariantStatusPending,
		RetryCount:   1,
		NextRetryAt:  &nextRetryAt,
		ErrorMessage: "retry later",
	}
	require.NoError(t, db.Create(variant).Error)

	tempDir := t.TempDir()
	count, err := backupTable(db, "image_variants", tempDir)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	data, err := os.ReadFile(filepath.Join(tempDir, "image_variants.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "\"format\":\"webp\"")
	assert.Contains(t, string(data), "\"retry_count\":1")
}

func TestRestoreTableRestoresImageVariants(t *testing.T) {
	db := setupBackupRestoreTestDB(t)

	image := &models.Image{
		ID:              42,
		Identifier:      "restore-image",
		StoragePath:     "original/restore.png",
		OriginalName:    "restore.png",
		FileSize:        321,
		MimeType:        "image/png",
		StorageConfigID: 1,
		FileHash:        "restore-hash",
		UserID:          1,
	}
	require.NoError(t, db.Create(image).Error)

	nextRetryAt := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)
	record := models.ImageVariant{
		ID:           7,
		ImageID:      image.ID,
		Format:       models.FormatThumbnailSize(600),
		Identifier:   "restore-image_600.webp",
		StoragePath:  "thumbnails/restore-image_600.webp",
		FileSize:     88,
		FileHash:     "restore-variant-hash",
		Width:        600,
		Height:       400,
		Status:       models.VariantStatusPending,
		RetryCount:   2,
		NextRetryAt:  &nextRetryAt,
		ErrorMessage: "stale retry",
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "image_variants.jsonl")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(file).Encode(record))
	require.NoError(t, file.Close())

	stats := newRestoreStats()
	require.NoError(t, restoreTable(db, "image_variants", path, stats, false))
	assert.Equal(t, int64(1), stats.Restored["image_variants"])

	var restored models.ImageVariant
	require.NoError(t, db.First(&restored, record.ID).Error)
	assert.Equal(t, record.Identifier, restored.Identifier)
	assert.Equal(t, record.StoragePath, restored.StoragePath)
	assert.Equal(t, record.RetryCount, restored.RetryCount)
	require.NotNil(t, restored.NextRetryAt)
	assert.True(t, restored.NextRetryAt.Equal(nextRetryAt))
}

func setupBackupRestoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Image{}, &models.ImageVariant{}))
	return db
}
