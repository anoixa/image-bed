package database

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAutoMigrateIndexFixesAreQuietWhenAlreadyApplied(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	require.NoError(t, AutoMigrate(db))
	assert.False(t, db.Migrator().HasIndex(&models.Image{}, "idx_identifier"))
	assert.False(t, db.Migrator().HasIndex(&models.Image{}, "idx_images_identifier"))
	assert.True(t, db.Migrator().HasIndex(&models.Image{}, "idx_images_identifier_active"))
	assert.True(t, db.Migrator().HasIndex(&models.SystemConfig{}, "idx_key_unique"))

	logs.Reset()
	require.NoError(t, AutoMigrate(db))

	output := logs.String()
	assert.NotContains(t, output, "Dropped old index")
	assert.NotContains(t, output, "Created new partial index idx_key_unique")
	assert.NotContains(t, output, "Created partial unique index idx_images_identifier_active")
	assert.False(t, db.Migrator().HasIndex(&models.Image{}, "idx_identifier"))
	assert.True(t, db.Migrator().HasIndex(&models.Image{}, "idx_images_identifier_active"))
}
