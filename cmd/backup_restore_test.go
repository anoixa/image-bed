package cmd

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
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
	spec, ok := database.TableByName("image_variants")
	require.True(t, ok)
	count, checksum, err := backupTable(db, spec, tempDir)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.NotEmpty(t, checksum)

	data, err := os.ReadFile(filepath.Join(tempDir, "image_variants.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "\"format\":\"webp\"")
	assert.Contains(t, string(data), "\"retry_count\":1")
}

// TestBackup_V2RoundTripPreservesSecrets is the regression test for F3: a v2
// backup must round-trip the json:"-" columns (password hash, encrypted config
// blob) that older backups silently dropped, producing unusable restored rows.
func TestBackup_V2RoundTripPreservesSecrets(t *testing.T) {
	srcDB := newRestoreTestDB(t)
	dataPath := t.TempDir()
	writeMasterKey(t, dataPath)

	user := &models.User{
		Username: "admin",
		Password: "$2a$10$abcdefghijklmnopqrstuv.SECREThashvalue1234567890abcd",
		Role:     "admin",
		Status:   "active",
	}
	require.NoError(t, srcDB.Create(user).Error)

	cfg := &models.SystemConfig{
		Category:   models.ConfigCategoryStorage,
		Name:       "primary",
		Key:        "storage:local:1",
		IsEnabled:  true,
		ConfigJSON: "__ENC:v2:ciphertextblobthatmustsurviveabackup",
	}
	require.NoError(t, srcDB.Create(cfg).Error)

	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := createBackupArchive(srcDB, backupOptions{outputFile: archivePath, dataPath: dataPath})
	require.NoError(t, err)
	require.Equal(t, currentBackupVersion, result.Metadata.Version)
	assert.NotEmpty(t, result.Metadata.EncryptionKeyFingerprint)
	assert.NotEmpty(t, result.Metadata.ArchiveMAC)

	// Restore into a fresh database.
	dstDB := newRestoreTestDB(t)
	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	meta, err := loadAndValidateMetadata(extractDir)
	require.NoError(t, err)
	major, err := archiveMajorVersion(meta.Version)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(meta, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveSecurity(extractDir, dataPath, meta))
	require.NoError(t, validateArchiveData(extractDir, meta, selected, major))
	_, err = executeRestore(dstDB, "sqlite", extractDir, meta, selected, major, true, true)
	require.NoError(t, err)

	var restoredUser models.User
	require.NoError(t, dstDB.Where("username = ?", "admin").First(&restoredUser).Error)
	assert.Equal(t, user.Password, restoredUser.Password, "password hash must survive the backup round-trip")

	var restoredCfg models.SystemConfig
	require.NoError(t, dstDB.Where("key = ?", "storage:local:1").First(&restoredCfg).Error)
	assert.Equal(t, cfg.ConfigJSON, restoredCfg.ConfigJSON, "encrypted config blob must survive the backup round-trip")
}

func TestArchiveSecurityRejectsWrongKeyAndMetadataTampering(t *testing.T) {
	db := newRestoreTestDB(t)
	require.NoError(t, db.Create(&models.SystemConfig{
		Category: models.ConfigCategoryStorage, Name: "encrypted", Key: "storage:test",
		IsEnabled: true, ConfigJSON: "__ENC:v2:ciphertext",
	}).Error)

	dataPath := t.TempDir()
	writeMasterKey(t, dataPath)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	_, err := createBackupArchive(db, backupOptions{outputFile: archivePath, dataPath: dataPath})
	require.NoError(t, err)

	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	meta, err := loadAndValidateMetadata(extractDir)
	require.NoError(t, err)
	require.NoError(t, validateArchiveSecurity(extractDir, dataPath, meta))

	wrongDataPath := t.TempDir()
	writeMasterKeyWithFill(t, wrongDataPath, 0x44)
	err = validateArchiveSecurity(extractDir, wrongDataPath, meta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fingerprint mismatch")

	meta.RecordCount["system_configs"]++
	err = validateArchiveSecurity(extractDir, dataPath, meta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

// TestBackup_DefaultIncludesAllManifestTables guards F3: a default backup must
// cover every durable data table, not the historical seven.
func TestBackup_DefaultIncludesAllManifestTables(t *testing.T) {
	db := newRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	result, err := createBackupArchive(db, backupOptions{outputFile: archivePath})
	require.NoError(t, err)

	var want []string
	for _, spec := range database.BackupTables() {
		want = append(want, spec.Name)
	}
	assert.ElementsMatch(t, want, result.Metadata.Tables)
	for _, table := range want {
		_, ok := result.Metadata.Checksums[table]
		assert.Truef(t, ok, "metadata must record a checksum for %q", table)
	}
}

// TestRestore_V2ChecksumMismatchRejected guards F6: a tampered v2 archive must
// be rejected before any database mutation.
func TestRestore_V2ChecksumMismatchRejected(t *testing.T) {
	srcDB := newRestoreTestDB(t)
	require.NoError(t, srcDB.Create(&models.User{Username: "u", Password: "p", Role: "user", Status: "active"}).Error)

	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	_, err := createBackupArchive(srcDB, backupOptions{outputFile: archivePath, tables: []string{"users"}})
	require.NoError(t, err)

	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	meta, err := loadAndValidateMetadata(extractDir)
	require.NoError(t, err)
	major, err := archiveMajorVersion(meta.Version)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(meta, nil)
	require.NoError(t, err)

	// Tamper one byte of the extracted data file.
	usersFile := filepath.Join(extractDir, "users.jsonl")
	content, err := os.ReadFile(usersFile)
	require.NoError(t, err)
	content[0] ^= 0xFF
	require.NoError(t, os.WriteFile(usersFile, content, 0600))

	err = validateArchiveData(extractDir, meta, selected, major)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

// TestBackup_FailsOnTableReadError guards F6: a table that cannot be read must
// abort the backup with an error rather than reporting a partial success.
func TestBackup_FailsOnTableReadError(t *testing.T) {
	// This DB only has the image/variant tables migrated, so reading
	// system_configs fails.
	db := setupBackupRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	_, err := createBackupArchive(db, backupOptions{
		outputFile: archivePath,
		tables:     []string{"system_configs"},
	})
	require.Error(t, err)
	assert.NoFileExists(t, archivePath, "no archive should be produced on failure")
}

func TestBackup_MasterKeyExcludedByDefault(t *testing.T) {
	db := newRestoreTestDB(t)
	dataPath := t.TempDir()
	writeMasterKey(t, dataPath)

	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := createBackupArchive(db, backupOptions{
		outputFile: archivePath,
		tables:     []string{"users"},
		dataPath:   dataPath,
	})
	require.NoError(t, err)
	assert.False(t, result.Metadata.MasterKeyIncluded)

	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	assert.NoFileExists(t, filepath.Join(extractDir, "master.key"))
}

func TestBackup_MasterKeyIncludedWithFlag(t *testing.T) {
	t.Setenv("CONFIG_ENCRYPTION_KEY", "")
	db := newRestoreTestDB(t)
	dataPath := t.TempDir()
	keyContent := writeMasterKey(t, dataPath)

	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := createBackupArchive(db, backupOptions{
		outputFile:       archivePath,
		tables:           []string{"system_configs", "user_totp_settings"},
		dataPath:         dataPath,
		includeMasterKey: true,
	})
	require.NoError(t, err)
	assert.True(t, result.Metadata.MasterKeyIncluded)

	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	got, err := os.ReadFile(filepath.Join(extractDir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, keyContent, got)
}

func TestBackup_MasterKeyPreflightRejectsUnsafeSourcesAndSelections(t *testing.T) {
	db := newRestoreTestDB(t)

	t.Run("environment managed key", func(t *testing.T) {
		t.Setenv("CONFIG_ENCRYPTION_KEY", string(base64Key(t, 0x22)))
		_, err := createBackupArchive(db, backupOptions{
			outputFile:       filepath.Join(t.TempDir(), "backup.tar.gz"),
			includeMasterKey: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "environment-managed key")
	})

	t.Run("partial key dependent tables", func(t *testing.T) {
		t.Setenv("CONFIG_ENCRYPTION_KEY", "")
		dataPath := t.TempDir()
		writeMasterKey(t, dataPath)
		_, err := createBackupArchive(db, backupOptions{
			outputFile:       filepath.Join(t.TempDir(), "backup.tar.gz"),
			tables:           []string{"system_configs"},
			dataPath:         dataPath,
			includeMasterKey: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires all key-dependent tables")
	})

	t.Run("missing key file", func(t *testing.T) {
		t.Setenv("CONFIG_ENCRYPTION_KEY", "")
		_, err := createBackupArchive(db, backupOptions{
			outputFile:       filepath.Join(t.TempDir(), "backup.tar.gz"),
			dataPath:         t.TempDir(),
			includeMasterKey: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no master key exists")
	})
}

// TestRestore_MasterKeyNotOverwrittenWithoutForce guards the decision that a
// bundled key must never silently replace an existing key.
func TestRestore_MasterKeyNotOverwrittenWithoutForce(t *testing.T) {
	t.Setenv("CONFIG_ENCRYPTION_KEY", "")
	extractDir := t.TempDir()
	archiveKey := base64Key(t, 0xAB)
	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), archiveKey, 0600))
	meta := &backupMetadata{MasterKeyIncluded: true}

	dataPath := t.TempDir()
	existing := writeMasterKey(t, dataPath)

	// With no encrypted table selected, the archive key is ignored.
	plan, err := validateBundledMasterKey(extractDir, dataPath, meta, false, false, []string{"users"})
	require.NoError(t, err)
	plan, err = stageMasterKeyFile(plan)
	require.NoError(t, err)
	require.NoError(t, commitMasterKeyFile(plan))
	got, err := os.ReadFile(filepath.Join(dataPath, cryptopackage.KeyDir, cryptopackage.MasterKeyFile))
	require.NoError(t, err)
	assert.Equal(t, existing, got, "existing key must not be overwritten without --restore-master-key")

	// A complete forced restore stages the replacement without touching the
	// destination, then atomically installs it after the DB transaction commits.
	plan, err = validateBundledMasterKey(extractDir, dataPath, meta, true, true, masterKeyDependentTables)
	require.NoError(t, err)
	plan, err = stageMasterKeyFile(plan)
	require.NoError(t, err)
	got, err = os.ReadFile(filepath.Join(dataPath, cryptopackage.KeyDir, cryptopackage.MasterKeyFile))
	require.NoError(t, err)
	assert.Equal(t, existing, got, "staging must not replace the active key before DB commit")
	assert.FileExists(t, plan.tempPath)
	require.NoError(t, commitMasterKeyFile(plan))
	got, err = os.ReadFile(filepath.Join(dataPath, cryptopackage.KeyDir, cryptopackage.MasterKeyFile))
	require.NoError(t, err)
	assert.Equal(t, archiveKey, got)
}

// TestRestore_MasterKeyValidationFailsBeforeMutation covers the pre-flight key
// checks that must reject an archive before any database change.
func TestRestore_MasterKeyValidationFailsBeforeMutation(t *testing.T) {
	t.Setenv("CONFIG_ENCRYPTION_KEY", "")
	t.Run("missing file", func(t *testing.T) {
		meta := &backupMetadata{MasterKeyIncluded: true}
		_, err := validateBundledMasterKey(t.TempDir(), t.TempDir(), meta, false, false, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing from the archive")
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), base64Key(t, 0xAB), 0600))
		meta := &backupMetadata{MasterKeyIncluded: true, MasterKeyChecksum: "deadbeef"}
		_, err := validateBundledMasterKey(extractDir, t.TempDir(), meta, false, false, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})

	t.Run("not 32 bytes", func(t *testing.T) {
		extractDir := t.TempDir()
		short := []byte(base64.StdEncoding.EncodeToString([]byte("too short")))
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), short, 0600))
		meta := &backupMetadata{MasterKeyIncluded: true}
		_, err := validateBundledMasterKey(extractDir, t.TempDir(), meta, false, false, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "32 bytes")
	})

	t.Run("incompatible key with system_configs", func(t *testing.T) {
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), base64Key(t, 0xAB), 0600))
		meta := &backupMetadata{MasterKeyIncluded: true}
		dataPath := t.TempDir()
		writeMasterKey(t, dataPath) // a different existing key
		_, err := validateBundledMasterKey(extractDir, dataPath, meta, false, false, []string{"system_configs"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "undecryptable")
	})

	t.Run("incompatible key with totp settings", func(t *testing.T) {
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), base64Key(t, 0xAB), 0600))
		meta := &backupMetadata{MasterKeyIncluded: true}
		dataPath := t.TempDir()
		writeMasterKey(t, dataPath)
		_, err := validateBundledMasterKey(extractDir, dataPath, meta, false, false, []string{"user_totp_settings"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "undecryptable")
	})

	t.Run("force requires complete truncated restore", func(t *testing.T) {
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), base64Key(t, 0xAB), 0600))
		meta := &backupMetadata{MasterKeyIncluded: true}
		_, err := validateBundledMasterKey(extractDir, t.TempDir(), meta, true, false, masterKeyDependentTables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires --truncate")
	})

	t.Run("environment key takes precedence over matching file", func(t *testing.T) {
		archiveKey := base64Key(t, 0xAB)
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), archiveKey, 0600))
		dataPath := t.TempDir()
		keyDir := filepath.Join(dataPath, cryptopackage.KeyDir)
		require.NoError(t, os.MkdirAll(keyDir, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(keyDir, cryptopackage.MasterKeyFile), archiveKey, 0600))
		t.Setenv("CONFIG_ENCRYPTION_KEY", string(base64Key(t, 0xCC)))

		_, err := validateBundledMasterKey(extractDir, dataPath, &backupMetadata{MasterKeyIncluded: true}, false, false, masterKeyDependentTables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "environment key takes precedence")
	})

	t.Run("validation is read only", func(t *testing.T) {
		extractDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(extractDir, "master.key"), base64Key(t, 0xAB), 0600))
		meta := &backupMetadata{MasterKeyIncluded: true}
		dataPath := filepath.Join(t.TempDir(), "not-created")
		plan, err := validateBundledMasterKey(extractDir, dataPath, meta, true, true, masterKeyDependentTables)
		require.NoError(t, err)
		assert.True(t, plan.write)
		assert.NoDirExists(t, dataPath)
	})
}

// TestBackup_ArchiveAndTempDirPermissions guards F6: the staging directory is
// 0700 and the archive (which holds password hashes and encrypted configs) is
// 0600.
func TestBackup_ArchiveAndTempDirPermissions(t *testing.T) {
	db := newRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	result, err := createBackupArchive(db, backupOptions{
		outputFile: archivePath,
		tables:     []string{"users"},
		keepDir:    true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(result.TempDir) })

	archiveInfo, err := os.Stat(archivePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), archiveInfo.Mode().Perm(), "archive must be 0600")

	dirInfo, err := os.Stat(result.TempDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm(), "staging dir must be 0700")
}

func TestBackup_KeepDirRetainsTemporaryFiles(t *testing.T) {
	db := setupBackupRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	result, err := createBackupArchive(db, backupOptions{
		outputFile: archivePath,
		tables:     []string{"image_variants"},
		keepDir:    true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(result.TempDir) })

	info, err := os.Stat(result.TempDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.FileExists(t, filepath.Join(result.TempDir, "metadata.json"))
	assert.FileExists(t, filepath.Join(result.TempDir, "image_variants.jsonl"))
}

func TestBackup_RemovesTempDirByDefault(t *testing.T) {
	db := setupBackupRestoreTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")

	result, err := createBackupArchive(db, backupOptions{
		outputFile: archivePath,
		tables:     []string{"image_variants"},
	})
	require.NoError(t, err)
	_, err = os.Stat(result.TempDir)
	require.ErrorIs(t, err, os.ErrNotExist, "temporary directory should be removed by default")
}

// base64Key returns a valid master-key file body: base64 of 32 identical bytes.
func base64Key(t *testing.T, fill byte) []byte {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = fill
	}
	return []byte(base64.StdEncoding.EncodeToString(raw))
}

// writeMasterKey writes a deterministic, valid master key file under
// dataPath/config and returns its bytes.
func writeMasterKey(t *testing.T, dataPath string) []byte {
	return writeMasterKeyWithFill(t, dataPath, 0x11)
}

func writeMasterKeyWithFill(t *testing.T, dataPath string, fill byte) []byte {
	t.Helper()
	dir := filepath.Join(dataPath, cryptopackage.KeyDir)
	require.NoError(t, os.MkdirAll(dir, 0700))
	content := base64Key(t, fill)
	require.NoError(t, os.WriteFile(filepath.Join(dir, cryptopackage.MasterKeyFile), content, 0600))
	return content
}

// TestBackup_PreservesSoftDeletedRows guards R1: soft-deleted rows must be in
// the backup and round-trip with their deleted_at intact. A plain Table().Find
// would have GORM apply the soft-delete scope and silently drop them.
func TestBackup_PreservesSoftDeletedRows(t *testing.T) {
	srcDB := newRestoreTestDB(t)

	live := &models.Image{
		Identifier: "live", StoragePath: "o/l.png", OriginalName: "l.png",
		FileSize: 1, MimeType: "image/png", StorageConfigID: 1, FileHash: "hl", UserID: 1,
	}
	require.NoError(t, srcDB.Create(live).Error)
	del := &models.Image{
		Identifier: "del", StoragePath: "o/d.png", OriginalName: "d.png",
		FileSize: 1, MimeType: "image/png", StorageConfigID: 1, FileHash: "hd", UserID: 1,
	}
	require.NoError(t, srcDB.Create(del).Error)
	require.NoError(t, srcDB.Delete(del).Error) // soft delete

	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := createBackupArchive(srcDB, backupOptions{outputFile: archivePath, tables: []string{"images"}})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Metadata.RecordCount["images"], "soft-deleted row must be backed up")

	dstDB := newRestoreTestDB(t)
	extractDir := t.TempDir()
	require.NoError(t, extractTarGz(archivePath, extractDir))
	meta, err := loadAndValidateMetadata(extractDir)
	require.NoError(t, err)
	major, err := archiveMajorVersion(meta.Version)
	require.NoError(t, err)
	selected, err := resolveRestoreTables(meta, nil)
	require.NoError(t, err)
	require.NoError(t, validateArchiveData(extractDir, meta, selected, major))
	_, err = executeRestore(dstDB, "sqlite", extractDir, meta, selected, major, false, true)
	require.NoError(t, err)

	var total int64
	require.NoError(t, dstDB.Unscoped().Model(&models.Image{}).Count(&total).Error)
	assert.Equal(t, int64(2), total)
	var deleted int64
	require.NoError(t, dstDB.Unscoped().Model(&models.Image{}).Where("deleted_at IS NOT NULL").Count(&deleted).Error)
	assert.Equal(t, int64(1), deleted, "the soft-deleted row must round-trip with its deleted_at")
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
