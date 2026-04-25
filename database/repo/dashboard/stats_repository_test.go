package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupDashboardTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Image{}))
	return db
}

func TestGetDailyStatsScansSQLiteDateString(t *testing.T) {
	db := setupDashboardTestDB(t)
	repo := NewRepository(db)

	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       yesterday,
		UpdatedAt:       yesterday,
		Identifier:      "img-1",
		StoragePath:     "original/img-1.jpg",
		OriginalName:    "img-1.jpg",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "hash-1",
		UserID:          1,
	}).Error)
	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       yesterday.Add(2 * time.Hour),
		UpdatedAt:       yesterday.Add(2 * time.Hour),
		Identifier:      "img-2",
		StoragePath:     "original/img-2.jpg",
		OriginalName:    "img-2.jpg",
		FileSize:        200,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "hash-2",
		UserID:          1,
	}).Error)

	stats, err := repo.GetDailyStats(context.Background(), 30, nil)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, yesterday.Format("2006-01-02"), stats[0].Date.Format("2006-01-02"))
	assert.Equal(t, int64(2), stats[0].Count)
}

func TestGetDailyStatsScopesByUser(t *testing.T) {
	db := setupDashboardTestDB(t)
	repo := NewRepository(db)

	now := time.Now()
	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       now,
		UpdatedAt:       now,
		Identifier:      "user-1-img",
		StoragePath:     "original/user-1-img.jpg",
		OriginalName:    "user-1-img.jpg",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "user-1-hash",
		UserID:          1,
	}).Error)
	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       now,
		UpdatedAt:       now,
		Identifier:      "user-2-img",
		StoragePath:     "original/user-2-img.jpg",
		OriginalName:    "user-2-img.jpg",
		FileSize:        200,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "user-2-hash",
		UserID:          2,
	}).Error)

	userID := uint(1)
	stats, err := repo.GetDailyStats(context.Background(), 30, &userID)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, int64(1), stats[0].Count)
}
