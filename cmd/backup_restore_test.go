package cmd

import (
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

// TestBackup_EndToEndRoundTrip verifies a real backup tar.gz can be restored.
// This test catches issues like the "." directory entry incompatibility (P1-1).
func TestBackup_EndToEndRoundTrip(t *testing.T) {
	srcDB := setupBackupRestoreTestDB(t)
	user := &models.User{Username: "test", Role: "user", Status: "active"}
	user.ID = 1
	require.NoError(t, srcDB.Create(user).Error)

	tempDir := t.TempDir()
	archivePath := filepath.Join(tempDir, "backup.tar.gz")

	// Run the full backup flow (creates metadata + tar.gz).
	tables := []string{"users"}
	result, err := createBackupArchive(srcDB, tables, archivePath, false)
	require.NoError(t, err)
	require.Equal(t, archivePath, result.OutputFile)
	require.Equal(t, int64(1), result.Metadata.RecordCount["users"])
	_, err = os.Stat(result.TempDir)
	require.ErrorIs(t, err, os.ErrNotExist, "temporary directory should be removed by default")

	// Now restore into a fresh database.
	dstDB := newRestoreTestDB(t)
	extractDir := t.TempDir()

	require.NoError(t, extractTarGz(archivePath, extractDir))
	meta, err := loadAndValidateMetadata(extractDir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(meta, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(extractDir, meta, selected))

	stats, err := executeRestore(dstDB, "sqlite", extractDir, meta, selected, true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Restored["users"])

	var restored models.User
	require.NoError(t, dstDB.First(&restored, 1).Error)
	assert.Equal(t, "test", restored.Username)
}

func TestBackup_KeepDirRetainsTemporaryFiles(t *testing.T) {
	db := setupBackupRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	result, err := createBackupArchive(db, []string{"image_variants"}, archivePath, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(result.TempDir) })

	info, err := os.Stat(result.TempDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.FileExists(t, filepath.Join(result.TempDir, "metadata.json"))
	assert.FileExists(t, filepath.Join(result.TempDir, "image_variants.jsonl"))
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
