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

// setupRepoWithImagesTestDB migrates ImageVariant AND Image (ListDuePendingImageIDs
// JOINs images and tests create image rows).
func setupRepoWithImagesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ImageVariant{}, &models.Image{}))
	return db
}

func createTestImage(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	require.NoError(t, db.Create(&models.Image{
		ID:              id,
		Identifier:      "img-" + string(rune('a'+int(id))),
		StoragePath:     "p",
		OriginalName:    "a.jpg",
		FileSize:        1024,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "hash-" + string(rune('a'+int(id))),
	}).Error)
}

func TestSetPendingBackoffDoesNotTouchRetryCount(t *testing.T) {
	db := setupRepoWithImagesTestDB(t)
	repo := NewVariantRepository(db)
	createTestImage(t, db, 1)

	v, err := repo.UpsertPending(1, models.FormatWebP)
	require.NoError(t, err)

	at := time.Now().Add(10 * time.Minute)
	require.NoError(t, repo.SetPendingBackoff([]uint{v.ID}, at))

	got, err := repo.GetByID(v.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, got.RetryCount, "backoff must not increment retry_count")
	require.NotNil(t, got.NextRetryAt, "next_retry_at must be set")
}

func TestListDuePendingImageIDs(t *testing.T) {
	db := setupRepoWithImagesTestDB(t)
	repo := NewVariantRepository(db)
	createTestImage(t, db, 1)

	v, err := repo.UpsertPending(1, models.FormatWebP)
	require.NoError(t, err)

	// future backoff -> not due
	require.NoError(t, repo.SetPendingBackoff([]uint{v.ID}, time.Now().Add(time.Hour)))
	ids, err := repo.ListDuePendingImageIDs(0, 100)
	require.NoError(t, err)
	assert.Empty(t, ids, "future backoff must not be due")

	// past -> due
	require.NoError(t, repo.SetPendingBackoff([]uint{v.ID}, time.Now().Add(-time.Minute)))
	ids, err = repo.ListDuePendingImageIDs(0, 100)
	require.NoError(t, err)
	assert.Equal(t, []uint{1}, ids)

	// cursor pagination excludes already-seen ids
	ids, err = repo.ListDuePendingImageIDs(1, 100)
	require.NoError(t, err)
	assert.Empty(t, ids, "cursor > last id must return nothing")
}
