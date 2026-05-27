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

func setTestLocalTimezone(t *testing.T) *time.Location {
	t.Helper()

	loc := time.FixedZone("TEST_CST", 8*3600)
	previous := time.Local
	time.Local = loc
	t.Cleanup(func() {
		time.Local = previous
	})
	return loc
}

func TestGetDailyStatsBucketsByAppTimezone(t *testing.T) {
	loc := setTestLocalTimezone(t)
	db := setupDashboardTestDB(t)
	repo := NewRepository(db)

	now := time.Now().In(loc)
	yesterdayLocal := startOfDay(now).AddDate(0, 0, -1).Add(12 * time.Hour)

	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       yesterdayLocal.UTC(),
		UpdatedAt:       yesterdayLocal.UTC(),
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
		CreatedAt:       yesterdayLocal.Add(2 * time.Hour).UTC(),
		UpdatedAt:       yesterdayLocal.Add(2 * time.Hour).UTC(),
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
	assert.Equal(t, yesterdayLocal.Format("2006-01-02"), stats[0].Date.Format("2006-01-02"))
	assert.Equal(t, int64(2), stats[0].Count)
}

func TestDashboardStatsBucketsUTCMidnightIntoLocalDay(t *testing.T) {
	loc := setTestLocalTimezone(t)
	db := setupDashboardTestDB(t)
	repo := NewRepository(db)

	now := time.Now().In(loc)
	todayStart := startOfDay(now)
	localMidnightUpload := todayStart.Add(30 * time.Minute)

	require.NoError(t, db.Create(&models.Image{
		CreatedAt:       localMidnightUpload.UTC(),
		UpdatedAt:       localMidnightUpload.UTC(),
		Identifier:      "midnight-img",
		StoragePath:     "original/midnight-img.jpg",
		OriginalName:    "midnight-img.jpg",
		FileSize:        100,
		MimeType:        "image/jpeg",
		StorageConfigID: 1,
		FileHash:        "midnight-hash",
		UserID:          1,
	}).Error)

	dailyStats, err := repo.GetDailyStats(context.Background(), 30, nil)
	require.NoError(t, err)
	require.Len(t, dailyStats, 1)
	assert.Equal(t, todayStart.Format("2006-01-02"), dailyStats[0].Date.Format("2006-01-02"))
	assert.Equal(t, int64(1), dailyStats[0].Count)

	timeStats, err := repo.GetImageTimeStats(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), timeStats.Today)
	assert.Equal(t, int64(0), timeStats.Yesterday)
}

func TestGetDailyStatsScopesByUser(t *testing.T) {
	setTestLocalTimezone(t)
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
