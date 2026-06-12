package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"

	"github.com/anoixa/image-bed/utils"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var backupLog = utils.ForModule("Backup")

// legacyV1DefaultBackupTables is the fixed table set written by the historical
// v1 backup format. The v2 writer no longer uses it as a default (it derives
// from the manifest), but restore keeps it to recognise a "complete" v1 archive.
var legacyV1DefaultBackupTables = []string{
	"users",
	"devices",
	"images",
	"image_variants",
	"albums",
	"album_images",
	"api_tokens",
}

// currentBackupVersion is the archive format this build writes. Restore reads
// both this and legacy "1.0" archives, selecting a codec by major version.
const currentBackupVersion = "2.1"

// masterKeyArchiveEntry is the archive member name used for a bundled master key.
const masterKeyArchiveEntry = "master.key"

// masterKeyDependentTables contain ciphertext encrypted by the application
// master key. A self-contained archive must include both tables so adopting the
// bundled key cannot leave part of the restored security state undecryptable.
var masterKeyDependentTables = []string{"system_configs", "user_totp_settings"}

// backupCmd 数据库备份命令
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup database to a JSONL tar.gz archive",
	Long: `Backup the database to JSONL files packed into a tar.gz archive.

The archive captures every durable table — including security-sensitive columns
(password hashes, encrypted configs) that older backups silently dropped — and
records a SHA-256 checksum per table so restore can detect corruption. All tables
are read inside a single transaction for a point-in-time consistent snapshot.
Archives containing encrypted rows also record the required encryption-key
fingerprint and an HMAC over the metadata and table checksums.

The archive always contains password hashes and encrypted config blobs, so it is
written with 0600 permissions. The master encryption key (master.key) is NOT
included unless --include-master-key is passed. Stored image objects are not part
of this database archive and must be backed up through the storage backend.

Example:
  # Backup to the default file (./data/backups/backup_YYYYMMDD_HHMMSS.tar.gz)
  image-bed backup

  # Backup to a specific file
  image-bed backup --output ./my-backup.tar.gz

  # Backup specific tables only
  image-bed backup --tables users,images

  # Include the master key (DANGEROUS: archive becomes self-decrypting)
  image-bed backup --include-master-key`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := initCommandLogger(); err != nil {
			exitWithErrorf("Failed to initialize config/logger: %v", err)
		}

		opts := backupOptions{}
		opts.outputFile, _ = cmd.Flags().GetString("output")
		opts.tables, _ = cmd.Flags().GetStringSlice("tables")
		opts.keepDir, _ = cmd.Flags().GetBool("keep-dir")
		opts.includeMasterKey, _ = cmd.Flags().GetBool("include-master-key")

		if err := runBackup(opts); err != nil {
			exitWithErrorf("Backup failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().StringP("output", "o", "", "Output tar.gz file path (default: ./data/backups/backup_YYYYMMDD_HHMMSS.tar.gz)")
	backupCmd.Flags().StringSliceP("tables", "t", []string{}, "Specific tables to backup (default: all durable tables)")
	backupCmd.Flags().Bool("keep-dir", false, "Keep temporary directory after creating archive")
	backupCmd.Flags().Bool("include-master-key", false, "Bundle the master encryption key into the archive (DANGEROUS: makes the archive self-decrypting)")
}

// backupOptions holds the resolved inputs for a backup run.
type backupOptions struct {
	tables           []string
	outputFile       string
	keepDir          bool
	includeMasterKey bool
	// dataPath is the root that holds config/master.key. Empty means the
	// production default (config.DefaultDataDir); tests override it.
	dataPath string
}

func (o backupOptions) resolvedDataPath() string {
	if o.dataPath != "" {
		return o.dataPath
	}
	return config.DefaultDataDir
}

// masterKeyPath returns the on-disk location of the master key file.
func (o backupOptions) masterKeyPath() string {
	return filepath.Join(o.resolvedDataPath(), cryptopackage.KeyDir, cryptopackage.MasterKeyFile)
}

// backupMetadata 备份元数据
type backupMetadata struct {
	Version     string           `json:"version"`
	Timestamp   time.Time        `json:"timestamp"`
	Database    string           `json:"database"`
	Tables      []string         `json:"tables"`
	RecordCount map[string]int64 `json:"record_count"`
	// Checksums maps each data table to the hex SHA-256 of its .jsonl file.
	// Empty for legacy v1 archives.
	Checksums map[string]string `json:"checksums,omitempty"`
	// MasterKeyIncluded is true when the archive bundles master.key.
	MasterKeyIncluded bool `json:"master_key_included"`
	// MasterKeyChecksum is the hex SHA-256 of the bundled master.key file, used
	// to detect corruption/tampering before a restore writes it. Empty when no
	// key is bundled.
	MasterKeyChecksum string `json:"master_key_checksum,omitempty"`
	// EncryptionKeyFingerprint identifies the effective config-encryption key
	// required to decrypt system configs and TOTP secrets without exposing it.
	EncryptionKeyFingerprint string `json:"encryption_key_fingerprint,omitempty"`
	// ArchiveMAC authenticates this metadata, including all table checksums,
	// using a domain-separated key derived from the config-encryption key.
	ArchiveMAC string `json:"archive_mac,omitempty"`
}

// initDB 初始化数据库连接
func initDB() (*gorm.DB, error) {
	cfg := config.Get()

	db, err := database.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return db, nil
}

// runBackup 执行备份
func runBackup(opts backupOptions) error {
	db, err := initDB()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()
	if err := recoverPendingMasterKey(db, opts.resolvedDataPath()); err != nil {
		return fmt.Errorf("failed to recover an interrupted restore before backup: %w", err)
	}

	if opts.outputFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		opts.outputFile = filepath.Join(config.DefaultDataDir, "backups", fmt.Sprintf("backup_%s.tar.gz", timestamp))
	}

	outputDir := filepath.Dir(opts.outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	backupLog.Infof("Starting backup to: %s", opts.outputFile)

	result, err := createBackupArchive(db, opts)
	if err != nil {
		return err
	}

	backupLog.Infof("Backup completed successfully: %s", result.OutputFile)
	if opts.keepDir {
		backupLog.Infof("Temporary backup directory retained: %s", result.TempDir)
	}
	printBackupSummary(result.Metadata, result.OutputFile)

	return nil
}

type backupArchiveResult struct {
	OutputFile string
	TempDir    string
	Metadata   *backupMetadata
}

// createBackupArchive is the testable core: given a DB and options, it builds a
// v2 archive and returns its path plus metadata. Every selected table is read
// inside one transaction so the dump is a point-in-time consistent snapshot, and
// any single-table failure aborts the whole backup (no partial-but-"successful"
// archive).
func createBackupArchive(db *gorm.DB, opts backupOptions) (*backupArchiveResult, error) {
	cfg := config.Get()

	tables, err := resolveBackupTables(opts.tables)
	if err != nil {
		return nil, err
	}

	var masterKeyData []byte
	if opts.includeMasterKey {
		masterKeyData, err = loadMasterKeyForBackup(opts, tables)
		if err != nil {
			return nil, err
		}
	}

	tempDir, err := os.MkdirTemp("", "image-bed-backup-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	// The directory holds password hashes and encrypted configs in the clear of
	// the archive's own encryption-at-rest, so keep it private.
	if err := os.Chmod(tempDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to secure temp directory: %w", err)
	}
	if !opts.keepDir {
		defer func() { _ = os.RemoveAll(tempDir) }()
	}

	metadata := &backupMetadata{
		Version:     currentBackupVersion,
		Timestamp:   time.Now(),
		Database:    cfg.DBType,
		Tables:      tables,
		RecordCount: make(map[string]int64, len(tables)),
		Checksums:   make(map[string]string, len(tables)),
	}

	// Read every table inside a single transaction for a consistent snapshot.
	// On PostgreSQL, escalate to REPEATABLE READ so concurrent writes cannot
	// produce a torn dump across tables.
	err = db.Transaction(func(tx *gorm.DB) error {
		if cfg.DBType == "postgres" || cfg.DBType == "postgresql" {
			if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; err != nil {
				return fmt.Errorf("failed to set snapshot isolation: %w", err)
			}
		}
		for _, table := range tables {
			spec, ok := database.TableByName(table)
			if !ok {
				return fmt.Errorf("unknown table: %s", table)
			}
			count, checksum, err := backupTable(tx, spec, tempDir)
			if err != nil {
				return fmt.Errorf("failed to backup table %s: %w", table, err)
			}
			metadata.RecordCount[table] = count
			metadata.Checksums[table] = checksum
			backupLog.Infof("Backed up %d records from table: %s", count, table)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(masterKeyData) > 0 {
		if err := bundleMasterKey(masterKeyData, tempDir, metadata); err != nil {
			return nil, err
		}
	}

	if archiveContainsKeyDependentData(metadata) {
		var configKey []byte
		if len(masterKeyData) > 0 {
			masterKey, err := decodeMasterKeyFile(masterKeyData)
			if err != nil {
				return nil, err
			}
			configKey, err = cryptopackage.DeriveConfigEncryptionKey(masterKey)
			if err != nil {
				return nil, err
			}
		} else {
			configKey, _, err = cryptopackage.LoadExistingConfigEncryptionKey(opts.resolvedDataPath())
			if err != nil {
				return nil, fmt.Errorf("backup contains encrypted data but its config encryption key is unavailable: %w", err)
			}
		}
		if err := applyArchiveSecurity(metadata, configKey); err != nil {
			return nil, fmt.Errorf("failed to authenticate archive metadata: %w", err)
		}
	}

	metadataPath := filepath.Join(tempDir, "metadata.json")
	if err := writeJSONFile(metadataPath, metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	// 打包成 tar.gz
	if err := createTarGz(tempDir, opts.outputFile); err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	return &backupArchiveResult{
		OutputFile: opts.outputFile,
		TempDir:    tempDir,
		Metadata:   metadata,
	}, nil
}

// resolveBackupTables returns the manifest-ordered table names to back up. An
// empty selection means every durable data table; an explicit selection must be
// a subset of the manifest's backup-data tables.
func resolveBackupTables(requested []string) ([]string, error) {
	if len(requested) == 0 {
		var tables []string
		for _, spec := range database.BackupTables() {
			tables = append(tables, spec.Name)
		}
		return tables, nil
	}

	want := make(map[string]bool, len(requested))
	for _, t := range requested {
		spec, ok := database.TableByName(t)
		if !ok || !spec.BackupData {
			return nil, fmt.Errorf("table %q is not a backup-data table in the manifest", t)
		}
		want[t] = true
	}
	// Preserve manifest (FK) order regardless of the order requested.
	var tables []string
	for _, spec := range database.BackupTables() {
		if want[spec.Name] {
			tables = append(tables, spec.Name)
		}
	}
	return tables, nil
}

func containsAllTables(selected, required []string) bool {
	want := make(map[string]bool, len(selected))
	for _, table := range selected {
		want[table] = true
	}
	for _, table := range required {
		if !want[table] {
			return false
		}
	}
	return true
}

func usesMasterKey(selected []string) bool {
	for _, table := range masterKeyDependentTables {
		for _, selectedTable := range selected {
			if table == selectedTable {
				return true
			}
		}
	}
	return false
}

// loadMasterKeyForBackup validates that --include-master-key can produce a
// genuinely self-contained archive. CONFIG_ENCRYPTION_KEY is already the final
// config encryption key, while a file master.key is HKDF-derived, so one cannot
// be serialized as the other without changing decryption semantics.
func loadMasterKeyForBackup(opts backupOptions, tables []string) ([]byte, error) {
	if os.Getenv("CONFIG_ENCRYPTION_KEY") != "" {
		return nil, errors.New("--include-master-key cannot be used while CONFIG_ENCRYPTION_KEY is set; back up the environment-managed key separately")
	}
	if !containsAllTables(tables, masterKeyDependentTables) {
		return nil, fmt.Errorf("--include-master-key requires all key-dependent tables: %s", strings.Join(masterKeyDependentTables, ", "))
	}

	keyPath := opts.masterKeyPath()
	data, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("--include-master-key was set but no master key exists at %s", keyPath)
		}
		return nil, fmt.Errorf("failed to read master key: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("master key file is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("master key file must decode to 32 bytes, got %d", len(raw))
	}
	return data, nil
}

// bundleMasterKey copies a validated master key into the archive staging
// directory and records it in the metadata.
func bundleMasterKey(data []byte, tempDir string, metadata *backupMetadata) error {
	dest := filepath.Join(tempDir, masterKeyArchiveEntry)
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return fmt.Errorf("failed to stage master key: %w", err)
	}
	sum := sha256.Sum256(data)
	metadata.MasterKeyIncluded = true
	metadata.MasterKeyChecksum = hex.EncodeToString(sum[:])
	backupLog.Warnf("⚠️  SECURITY: the master encryption key is bundled in this archive. " +
		"Anyone with the archive can decrypt all stored secrets. Store it with the same " +
		"protection as the key itself and never commit or share it.")
	return nil
}

// backupTable streams a table's rows into a JSONL file using the v2 backup
// record (which preserves every physical column, including the json:"-" ones),
// and returns the row count and the hex SHA-256 of the written file. It reads
// the physical table (no soft-delete scope), matching restore's row accounting.
func backupTable(tx *gorm.DB, spec database.TableSpec, tempDir string) (int64, string, error) {
	outputPath := filepath.Join(tempDir, spec.Name+".jsonl")
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(file, hasher))

	slicePtr := spec.V2RecordSlice()
	// Unscoped: the record types embed gorm.DeletedAt, so a plain Find would have
	// GORM apply the soft-delete scope (WHERE deleted_at IS NULL) and silently
	// drop soft-deleted rows. A backup must capture the physical table verbatim.
	if err := tx.Unscoped().Table(spec.Name).Find(slicePtr).Error; err != nil {
		return 0, "", err
	}

	rows := reflect.ValueOf(slicePtr).Elem()
	var count int64
	for i := 0; i < rows.Len(); i++ {
		if err := encoder.Encode(rows.Index(i).Interface()); err != nil {
			return 0, "", err
		}
		count++
	}

	return count, hex.EncodeToString(hasher.Sum(nil)), nil
}

// writeJSONFile 写入 JSON 文件
func writeJSONFile(path string, data any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// createTarGz writes sourceDir into a gzip-compressed tar archive at targetFile.
// It writes to a temporary file in the same directory, explicitly closes every
// writer layer while checking errors (a Close flushes buffered data — an ignored
// error there can leave a truncated archive that still "succeeds"), and only
// renames the finished file into place on full success. A failure leaves no
// partial archive behind. The archive carries password hashes and encrypted
// configs (and optionally the master key), so it is created 0600.
func createTarGz(sourceDir, targetFile string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(targetFile), ".backup-*.tar.gz.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0600); err != nil {
		return err
	}

	gzWriter := gzip.NewWriter(tmp)
	tarWriter := tar.NewWriter(gzWriter)

	if err := writeTarTree(tarWriter, sourceDir); err != nil {
		return err
	}

	// Close each layer in order, surfacing the error: tar -> gzip -> file.
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("failed to finalize tar stream: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("failed to finalize gzip stream: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to flush archive to disk: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close archive: %w", err)
	}

	if err := os.Rename(tmpName, targetFile); err != nil {
		return fmt.Errorf("failed to finalize archive: %w", err)
	}
	committed = true
	return nil
}

// writeTarTree walks sourceDir and writes every entry into tarWriter, rejecting
// nothing (the staging directory is trusted, built by this process).
func writeTarTree(tarWriter *tar.Writer, sourceDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		// Skip the root directory entry (".")
		if relPath == "." {
			return nil
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			data, err := os.Open(path)
			if err != nil {
				return err
			}
			defer func() { _ = data.Close() }()

			if _, err := io.Copy(tarWriter, data); err != nil {
				return err
			}
		}

		return nil
	})
}

// printBackupSummary 打印备份摘要
func printBackupSummary(metadata *backupMetadata, outputFile string) {
	fmt.Println("\nBackup Summary:")
	fmt.Println("===============")
	fmt.Printf("Version:    %s\n", metadata.Version)
	fmt.Printf("Timestamp:  %s\n", metadata.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Database:   %s\n", metadata.Database)
	fmt.Printf("Output:     %s\n", outputFile)
	fmt.Printf("Master key: %v\n", metadata.MasterKeyIncluded)
	fmt.Println("\nTables backed up:")
	for _, table := range metadata.Tables {
		count := metadata.RecordCount[table]
		fmt.Printf("  - %s: %d records\n", table, count)
	}
	var total int64
	for _, count := range metadata.RecordCount {
		total += count
	}
	fmt.Printf("\nTotal records: %d\n", total)
}
