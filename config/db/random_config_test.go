package config

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRandomSourceAlbumPersistsAcrossManagerRestart(t *testing.T) {
	db, dataDir := newRandomConfigTestDB(t)

	manager := newRandomConfigTestManager(t, db, dataDir)
	require.NoError(t, manager.SetRandomSourceAlbum(42, false, false))

	restarted := newRandomConfigTestManager(t, db, dataDir)
	assert.Equal(t, uint(42), restarted.GetRandomSourceAlbum())
	assert.False(t, restarted.GetRandomIncludeAllPublic())
	assert.False(t, restarted.GetRandomAPIEnabled())
}

func TestRandomAPIEnabledDefaultsToTrue(t *testing.T) {
	db, dataDir := newRandomConfigTestDB(t)

	manager := newRandomConfigTestManager(t, db, dataDir)

	assert.True(t, manager.GetRandomAPIEnabled())
}

func TestRandomSourceAlbumMigratesLegacyDoubleSystemKey(t *testing.T) {
	db, dataDir := newRandomConfigTestDB(t)
	manager := newRandomConfigTestManager(t, db, dataDir)

	encrypted, err := manager.crypto.Encrypt(map[string]any{
		"album_id":           77,
		"include_all_public": false,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.SystemConfig{
		Category:   models.ConfigCategorySystem,
		Name:       RandomSourceAlbumConfigKey,
		Key:        legacyRandomSourceAlbumConfigKey,
		ConfigJSON: encrypted,
		IsEnabled:  true,
	}).Error)

	restarted := newRandomConfigTestManager(t, db, dataDir)
	assert.Equal(t, uint(77), restarted.GetRandomSourceAlbum())
	assert.True(t, restarted.GetRandomAPIEnabled())

	migrated, err := restarted.repo.GetByKey(context.Background(), RandomSourceAlbumConfigKey)
	require.NoError(t, err)
	assert.Equal(t, randomSourceAlbumConfigName, migrated.Name)
}

func newRandomConfigTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()

	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemConfig{}))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db, dataDir
}

func newRandomConfigTestManager(t *testing.T, db *gorm.DB, dataDir string) *Manager {
	t.Helper()

	manager := NewManager(db, dataDir)
	require.NoError(t, manager.crypto.Initialize())
	return manager
}
