package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openSQLiteFile opens (and optionally migrates) a file-backed SQLite database.
func openSQLiteFile(t *testing.T, path string, migrate bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	if migrate {
		require.NoError(t, database.AutoMigrate(db))
	}
	return db
}

func closeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// seedAllTables creates exactly one row in every MigrateData table, with valid
// references, so a migration can be asserted table-by-table.
func seedAllTables(t *testing.T, db *gorm.DB) {
	t.Helper()

	user := &models.User{Username: "admin", Password: "HASHED-SECRET", Role: "admin", Status: "active"}
	user.ID = 1
	require.NoError(t, db.Create(user).Error)

	device := &models.Device{UserID: 1, RefreshToken: "rt-1", DeviceID: "dev-1", Expiry: time.Now().Add(time.Hour)}
	require.NoError(t, db.Create(device).Error)

	img := &models.Image{
		Identifier: "img-1", StoragePath: "o/1.png", OriginalName: "1.png",
		FileSize: 10, MimeType: "image/png", StorageConfigID: 1, FileHash: "h1", UserID: 1,
	}
	img.ID = 1
	require.NoError(t, db.Create(img).Error)

	variant := &models.ImageVariant{
		ImageID: 1, Format: models.FormatWebP, Identifier: "img-1.webp",
		StoragePath: "c/1.webp", FileSize: 5, FileHash: "vh1", Status: models.VariantStatusCompleted,
	}
	require.NoError(t, db.Create(variant).Error)

	album := &models.Album{UserID: 1, Name: "a"}
	album.ID = 1
	require.NoError(t, db.Create(album).Error)
	require.NoError(t, db.Exec("INSERT INTO album_images (album_id, image_id) VALUES (1, 1)").Error)

	token := &models.ApiToken{UserID: 1, IsActive: true, Token: "tok-secret", TokenPrefix: "tok"}
	require.NoError(t, db.Create(token).Error)

	cfg := &models.SystemConfig{
		Category: models.ConfigCategoryStorage, Name: "primary", Key: "storage:local:1",
		IsEnabled: true, ConfigJSON: "__ENC:v2:secret-config-blob",
	}
	require.NoError(t, db.Create(cfg).Error)

	identity := &models.UserIdentity{UserID: 1, Provider: "github", Subject: "gh-123", Email: "a@b.c"}
	require.NoError(t, db.Create(identity).Error)

	totp := &models.UserTOTPSetting{UserID: 1, SecretEncrypted: "enc-secret", Enabled: true}
	require.NoError(t, db.Create(totp).Error)
}

// TestMigrate_CoversAllManifestTables guards F5: a migration must copy every
// MigrateData table — including system_configs, user_identities and
// user_totp_settings, which the old hardcoded list silently omitted — and must
// preserve secret columns (password hash, encrypted config).
func TestMigrate_CoversAllManifestTables(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	seedAllTables(t, src)
	closeDB(t, src) // flush before the migration reopens it

	require.NoError(t, runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "skip"))

	dst := openSQLiteFile(t, dstPath, false)
	defer closeDB(t, dst)

	for _, spec := range database.MigrateTables() {
		var n int64
		require.NoError(t, dst.Table(spec.Name).Count(&n).Error)
		assert.Equalf(t, int64(1), n, "table %q must have been migrated", spec.Name)
	}

	// Secret columns must survive.
	var u models.User
	require.NoError(t, dst.Where("username = ?", "admin").First(&u).Error)
	assert.Equal(t, "HASHED-SECRET", u.Password)

	var cfg models.SystemConfig
	require.NoError(t, dst.Where("key = ?", "storage:local:1").First(&cfg).Error)
	assert.Equal(t, "__ENC:v2:secret-config-blob", cfg.ConfigJSON)
}

// TestMigrate_ConflictErrorPropagates guards that a real conflict under the
// "error" strategy is surfaced, not silently swallowed (the F5 behavior where
// any source error was treated as "table absent" and returned nil).
func TestMigrate_ConflictErrorPropagates(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	u := &models.User{Username: "admin", Password: "x", Role: "admin", Status: "active"}
	u.ID = 1
	require.NoError(t, src.Create(u).Error)
	closeDB(t, src)

	// Pre-seed the target with a conflicting user id=1.
	dst := openSQLiteFile(t, dstPath, true)
	existing := &models.User{Username: "existing", Password: "y", Role: "user", Status: "active"}
	existing.ID = 1
	require.NoError(t, dst.Create(existing).Error)
	closeDB(t, dst)

	err := runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "error")
	require.Error(t, err)
}

func TestMigrate_RollsBackEarlierTablesOnLaterFailure(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	sourceUser := &models.User{Username: "source", Password: "x", Role: "user", Status: "active"}
	sourceUser.ID = 1
	require.NoError(t, src.Create(sourceUser).Error)
	sourceDevice := &models.Device{UserID: 1, RefreshToken: "source-token", DeviceID: "source-device", Expiry: time.Now().Add(time.Hour)}
	sourceDevice.ID = 1
	require.NoError(t, src.Create(sourceDevice).Error)
	closeDB(t, src)

	dst := openSQLiteFile(t, dstPath, true)
	targetUser := &models.User{Username: "target", Password: "y", Role: "user", Status: "active"}
	targetUser.ID = 99
	require.NoError(t, dst.Create(targetUser).Error)
	targetDevice := &models.Device{UserID: 99, RefreshToken: "target-token", DeviceID: "target-device", Expiry: time.Now().Add(time.Hour)}
	targetDevice.ID = 1
	require.NoError(t, dst.Create(targetDevice).Error)
	closeDB(t, dst)

	err := runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "error")
	require.Error(t, err)

	dst = openSQLiteFile(t, dstPath, false)
	defer closeDB(t, dst)
	var count int64
	require.NoError(t, dst.Table("users").Where("id = ?", 1).Count(&count).Error)
	assert.Zero(t, count, "user inserted before the device conflict must be rolled back")
}

func TestMigrateJoinTableRejectsMissingParents(t *testing.T) {
	source := openSQLiteFile(t, filepath.Join(t.TempDir(), "src.db"), true)
	defer closeDB(t, source)
	target := openSQLiteFile(t, filepath.Join(t.TempDir(), "dst.db"), true)
	defer closeDB(t, target)

	require.NoError(t, source.Exec("INSERT INTO album_images (album_id, image_id) VALUES (?, ?)", 10, 20).Error)
	spec, ok := database.TableByName("album_images")
	require.True(t, ok)

	err := migrateJoinTable(context.Background(), source, target, spec, newMigrateStats(), "skip")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing target row")
}

func TestMigrate_RollsBackSchemaOnDataFailure(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	require.NoError(t, src.Exec("INSERT INTO album_images (album_id, image_id) VALUES (?, ?)", 10, 20).Error)
	closeDB(t, src)

	err := runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "skip")
	require.Error(t, err)

	dst := openSQLiteFile(t, dstPath, false)
	defer closeDB(t, dst)
	assert.False(t, dst.Migrator().HasTable(&models.User{}), "schema created inside the failed target transaction must roll back")
}

// TestMigrate_PreservesSoftDeletedRows guards R1: migration must copy
// soft-deleted rows (Unscoped read), not silently drop them.
func TestMigrate_PreservesSoftDeletedRows(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	live := &models.User{Username: "live", Password: "x", Role: "user", Status: "active"}
	live.ID = 1
	require.NoError(t, src.Create(live).Error)
	del := &models.User{Username: "del", Password: "y", Role: "user", Status: "active"}
	del.ID = 2
	require.NoError(t, src.Create(del).Error)
	require.NoError(t, src.Delete(del).Error) // soft delete
	closeDB(t, src)

	require.NoError(t, runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "skip"))

	dst := openSQLiteFile(t, dstPath, false)
	defer closeDB(t, dst)

	var total int64
	require.NoError(t, dst.Unscoped().Model(&models.User{}).Count(&total).Error)
	assert.Equal(t, int64(2), total)
	var deleted int64
	require.NoError(t, dst.Unscoped().Model(&models.User{}).Where("deleted_at IS NOT NULL").Count(&deleted).Error)
	assert.Equal(t, int64(1), deleted, "soft-deleted row must migrate with its deleted_at")
}

// TestMigrate_ResetsSequenceForNextInsert guards R4: after migrating rows with
// explicit ids, the target's auto-increment sequence must be advanced past them
// so the next insert does not collide with a migrated id.
func TestMigrate_ResetsSequenceForNextInsert(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.db")
	dstPath := filepath.Join(t.TempDir(), "dst.db")

	src := openSQLiteFile(t, srcPath, true)
	u := &models.User{Username: "restored", Password: "x", Role: "user", Status: "active"}
	u.ID = 5
	require.NoError(t, src.Create(u).Error)
	closeDB(t, src)

	require.NoError(t, runMigration("sqlite", "sqlite", srcPath, dstPath, "", "", true, 100, "skip"))

	dst := openSQLiteFile(t, dstPath, false)
	defer closeDB(t, dst)

	next := &models.User{Username: "after", Password: "z", Role: "user", Status: "active"}
	require.NoError(t, dst.Create(next).Error)
	assert.Greater(t, next.ID, uint(5), "next inserted id must be above the migrated maximum")
}

// TestMigrate_PostgresRoundTrip exercises a real PostgreSQL target (including
// sequence reset) when IMAGEBED_TEST_POSTGRES_DSN is set; it is skipped
// otherwise, since unit CI has no PostgreSQL server.
func TestMigrate_PostgresRoundTrip(t *testing.T) {
	dsn := os.Getenv("IMAGEBED_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set IMAGEBED_TEST_POSTGRES_DSN to run the PostgreSQL migration test")
	}

	srcPath := filepath.Join(t.TempDir(), "src.db")
	src := openSQLiteFile(t, srcPath, true)
	seedAllTables(t, src)
	closeDB(t, src)

	require.NoError(t, runMigration("sqlite", "postgres", srcPath, dsn, "", "", true, 100, "overwrite"))

	dst, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	defer closeDB(t, dst)

	for _, spec := range database.MigrateTables() {
		var n int64
		require.NoError(t, dst.Table(spec.Name).Count(&n).Error)
		assert.GreaterOrEqualf(t, n, int64(1), "table %q must have been migrated", spec.Name)
	}
	// Sequence advanced past the migrated user id.
	next := &models.User{Username: "pg-after", Password: "z", Role: "user", Status: "active"}
	require.NoError(t, dst.Create(next).Error)
	assert.Greater(t, next.ID, uint(1))
}
