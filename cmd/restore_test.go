package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newRestoreTestDB returns a fully migrated SQLite database backed by a unique
// per-test file (avoiding the shared in-memory pitfall where each pooled
// connection would see a different database).
func newRestoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "restore-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(db))
	return db
}

// jsonlLines encodes records as newline-delimited JSON, matching the v1 archive
// serialization (one json.Encoder line per record).
func jsonlLines(t *testing.T, records ...any) string {
	t.Helper()
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, r := range records {
		require.NoError(t, enc.Encode(r))
	}
	return b.String()
}

// writeArchiveDir builds an extracted-archive directory (metadata.json plus the
// given table files) independently of the production backup writer.
func writeArchiveDir(t *testing.T, meta backupMetadata, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	mb, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), mb, 0600))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
	}
	return dir
}

func v1Meta(tables []string, counts map[string]int64) backupMetadata {
	return backupMetadata{
		Version:     "1.0",
		Database:    "sqlite",
		Tables:      tables,
		RecordCount: counts,
	}
}

func sampleUser(id uint, name string) models.User {
	u := models.User{Username: name, Role: "user", Status: "active"}
	u.ID = id
	return u
}

func countRows(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Table(table).Count(&n).Error)
	return n
}

// TestRestore_ValidArchiveTruncateIntoEmptyDB is the happy path: a valid v1
// archive restored with --truncate into an empty database.
func TestRestore_ValidArchiveTruncateIntoEmptyDB(t *testing.T) {
	db := newRestoreTestDB(t)

	meta := v1Meta(
		[]string{"users", "images"},
		map[string]int64{"users": 2, "images": 1},
	)
	img := models.Image{
		Identifier: "img1", StoragePath: "o/1.png", OriginalName: "1.png",
		FileSize: 10, MimeType: "image/png", StorageConfigID: 1, FileHash: "h1", UserID: 1,
	}
	img.ID = 1
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl":  jsonlLines(t, sampleUser(1, "admin"), sampleUser(2, "bob")),
		"images.jsonl": jsonlLines(t, img),
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	stats, err := executeRestore(db, "sqlite", dir, m, selected, true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Restored["users"])
	assert.Equal(t, int64(1), stats.Restored["images"])
	assert.Equal(t, int64(2), countRows(t, db, "users"))
	assert.Equal(t, int64(1), countRows(t, db, "images"))
}

func TestRestore_CorruptJSONLFailsBeforeMutation(t *testing.T) {
	db := newRestoreTestDB(t)
	require.NoError(t, db.Create(&[]models.User{sampleUser(1, "keep")}).Error)

	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": "{not valid json}\n",
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)

	err = validateArchiveData(dir, m, selected)
	require.Error(t, err)
	// The pre-existing row is untouched because validation precedes mutation.
	assert.Equal(t, int64(1), countRows(t, db, "users"))
}

func TestRestore_MissingDataFileFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{}) // no users.jsonl

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)

	err = validateArchiveData(dir, m, selected)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing the data file")
}

func TestRestore_CountMismatchFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 5})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": jsonlLines(t, sampleUser(1, "a")), // only 1 record, metadata says 5
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)

	err = validateArchiveData(dir, m, selected)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects 5")
}

func TestRestore_UnsupportedVersionFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 0})
	meta.Version = "9.9"
	dir := writeArchiveDir(t, meta, map[string]string{"users.jsonl": ""})

	_, err := loadAndValidateMetadata(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported archive version")
}

func TestRestore_DuplicateMetadataTableFails(t *testing.T) {
	meta := v1Meta([]string{"users", "users"}, map[string]int64{"users": 0})
	dir := writeArchiveDir(t, meta, map[string]string{"users.jsonl": ""})

	_, err := loadAndValidateMetadata(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate table")
}

func TestRestore_UnknownRequestedTableFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 0})

	// Not in the manifest at all.
	_, err := resolveRestoreTables(&meta, []string{"not_a_table"})
	require.Error(t, err)

	// A real schema-only table is not a restorable data table.
	_, err = resolveRestoreTables(&meta, []string{"two_factor_challenges"})
	require.Error(t, err)

	// A valid data table that is absent from the archive.
	_, err = resolveRestoreTables(&meta, []string{"images"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not present in the archive")
}

func TestRestore_DefaultRejectsSchemaOnlyArchiveTable(t *testing.T) {
	meta := v1Meta([]string{"two_factor_challenges"}, map[string]int64{"two_factor_challenges": 0})

	_, err := resolveRestoreTables(&meta, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a restorable data table")
}

func TestRestore_OversizedLineFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	huge := `{"username":"` + strings.Repeat("a", maxJSONLLineBytes+1) + `"}` + "\n"
	dir := writeArchiveDir(t, meta, map[string]string{"users.jsonl": huge})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)

	err = validateArchiveData(dir, m, selected)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line limit")
}

func TestRestore_BlankLineFails(t *testing.T) {
	meta := v1Meta([]string{"users"}, map[string]int64{"users": 2})
	content := jsonlLines(t, sampleUser(1, "a")) + "\n" + jsonlLines(t, sampleUser(2, "b"))
	dir := writeArchiveDir(t, meta, map[string]string{"users.jsonl": content})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)

	err = validateArchiveData(dir, m, selected)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank line")
}

// TestRestore_ConstraintFailureRollsBack verifies that an insert-time database
// error after truncation rolls everything back, preserving the original rows.
func TestRestore_ConstraintFailureRollsBack(t *testing.T) {
	db := newRestoreTestDB(t)
	require.NoError(t, db.Create(&[]models.User{sampleUser(1, "original")}).Error)

	// image_variants has a unique index on (image_id, format); two identical
	// rows force a constraint violation during insert.
	dupVariant := func(id uint) models.ImageVariant {
		v := models.ImageVariant{
			ImageID: 1, Format: models.FormatWebP, Identifier: "x.webp",
			StoragePath: "p", FileSize: 1, FileHash: "h", Status: models.VariantStatusPending,
		}
		v.ID = id
		return v
	}
	meta := v1Meta(
		[]string{"users", "image_variants"},
		map[string]int64{"users": 1, "image_variants": 2},
	)
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl":          jsonlLines(t, sampleUser(2, "fromarchive")),
		"image_variants.jsonl": jsonlLines(t, dupVariant(10), dupVariant(11)),
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	_, err = executeRestore(db, "sqlite", dir, m, selected, true, true)
	require.Error(t, err)

	// Rolled back: the original user survives and the archive user is absent.
	assert.Equal(t, int64(1), countRows(t, db, "users"))
	var u models.User
	require.NoError(t, db.First(&u, 1).Error)
	assert.Equal(t, "original", u.Username)
	assert.Equal(t, int64(0), countRows(t, db, "image_variants"))
}

// TestRestore_MergeConflictErrors verifies that a primary-key conflict during a
// non-truncate (merge) restore is a hard error, not a silent skip.
func TestRestore_MergeConflictErrors(t *testing.T) {
	db := newRestoreTestDB(t)
	require.NoError(t, db.Create(&[]models.User{sampleUser(1, "existing")}).Error)

	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": jsonlLines(t, sampleUser(1, "conflict")),
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, []string{"users"})
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	_, err = executeRestore(db, "sqlite", dir, m, selected, false, false)
	require.Error(t, err)
}

// TestRestore_AlbumImagesExact verifies the composite-key join table restores
// its rows exactly.
func TestRestore_AlbumImagesExact(t *testing.T) {
	db := newRestoreTestDB(t)

	img := models.Image{
		Identifier: "i", StoragePath: "o/i.png", OriginalName: "i.png",
		FileSize: 1, MimeType: "image/png", StorageConfigID: 1, FileHash: "hh", UserID: 1,
	}
	img.ID = 1
	album := models.Album{UserID: 1, Name: "a"}
	album.ID = 1

	meta := v1Meta(
		[]string{"users", "images", "albums", "album_images"},
		map[string]int64{"users": 1, "images": 1, "albums": 1, "album_images": 1},
	)
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl":        jsonlLines(t, sampleUser(1, "u")),
		"images.jsonl":       jsonlLines(t, img),
		"albums.jsonl":       jsonlLines(t, album),
		"album_images.jsonl": `{"album_id":1,"image_id":1}` + "\n",
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	stats, err := executeRestore(db, "sqlite", dir, m, selected, true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Restored["album_images"])
	assert.Equal(t, int64(1), countRows(t, db, "album_images"))
}

// TestRestore_FullTruncateClearsEphemeral verifies a default full restore clears
// two_factor_challenges even though it is never present in the archive.
func TestRestore_FullTruncateClearsEphemeral(t *testing.T) {
	db := newRestoreTestDB(t)
	challenge := models.TwoFactorChallenge{TicketHash: "abc", UserID: 1}
	require.NoError(t, db.Create(&challenge).Error)
	require.Equal(t, int64(1), countRows(t, db, "two_factor_challenges"))

	counts := make(map[string]int64, len(legacyV1DefaultBackupTables))
	files := make(map[string]string, len(legacyV1DefaultBackupTables))
	for _, table := range legacyV1DefaultBackupTables {
		counts[table] = 0
		files[table+".jsonl"] = ""
	}
	counts["users"] = 1
	files["users.jsonl"] = jsonlLines(t, sampleUser(1, "u"))
	meta := v1Meta(append([]string(nil), legacyV1DefaultBackupTables...), counts)
	dir := writeArchiveDir(t, meta, files)

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	fullRestore := archiveIsComplete(m)
	require.True(t, fullRestore)
	_, err = executeRestore(db, "sqlite", dir, m, selected, fullRestore, true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), countRows(t, db, "two_factor_challenges"))
}

func TestRestore_PartialArchiveDoesNotClearEphemeral(t *testing.T) {
	db := newRestoreTestDB(t)
	challenge := models.TwoFactorChallenge{TicketHash: "keep", UserID: 1}
	require.NoError(t, db.Create(&challenge).Error)

	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": jsonlLines(t, sampleUser(1, "u")),
	})
	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	fullRestore := archiveIsComplete(m)
	require.False(t, fullRestore)
	_, err = executeRestore(db, "sqlite", dir, m, selected, fullRestore, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), countRows(t, db, "two_factor_challenges"))
}

func TestExtractTarGzAcceptsLegacyRootDirectoryEntry(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.tar.gz")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     ".",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}))
	payload := []byte("{\"version\":\"1.0\"}")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "metadata.json",
		Typeflag: tar.TypeReg,
		Mode:     0600,
		Size:     int64(len(payload)),
	}))
	_, err = tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())

	dest := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, dest))
	assert.FileExists(t, filepath.Join(dest, "metadata.json"))
}

// TestRestore_ValidationIsReadOnly underpins --dry-run: validation must not
// mutate any rows.
func TestRestore_ValidationIsReadOnly(t *testing.T) {
	db := newRestoreTestDB(t)
	require.NoError(t, db.Create(&[]models.User{sampleUser(1, "a"), sampleUser(2, "b")}).Error)

	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": jsonlLines(t, sampleUser(9, "fromarchive")),
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	assert.Equal(t, int64(2), countRows(t, db, "users"), "validation must not change the database")
}

// TestRestore_SequenceResetAllowsNextInsert verifies the auto-increment counter
// is moved past the restored maximum id.
func TestRestore_SequenceResetAllowsNextInsert(t *testing.T) {
	db := newRestoreTestDB(t)

	meta := v1Meta([]string{"users"}, map[string]int64{"users": 1})
	dir := writeArchiveDir(t, meta, map[string]string{
		"users.jsonl": jsonlLines(t, sampleUser(5, "restored")),
	})

	m, err := loadAndValidateMetadata(dir)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(m, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(dir, m, selected))

	_, err = executeRestore(db, "sqlite", dir, m, selected, true, true)
	require.NoError(t, err)

	next := models.User{Username: "after", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&next).Error)
	assert.Greater(t, next.ID, uint(5), "next inserted id must be above the restored maximum")
}
