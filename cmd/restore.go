package cmd

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/utils"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var restoreLog = utils.ForModule("Restore")

const (
	// supportedArchiveVersion is the only archive format this build can restore.
	// The v2 format (structured per-table checksums + assets) is introduced in a
	// later phase alongside its writer; until then non-1.0 archives are rejected.
	supportedArchiveVersion = "1.0"

	restoreBatchSize = 100

	// Bounds to keep a single oversized/expanding archive from exhausting disk
	// or memory during extraction and JSONL decoding.
	maxJSONLLineBytes    = 16 << 20       // 16 MiB per record line
	maxArchiveFileBytes  = int64(4) << 30 // 4 GiB per extracted file
	maxArchiveTotalBytes = int64(8) << 30 // 8 GiB total expanded
	maxArchiveEntries    = 256
)

// restoreCmd 数据库还原命令
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore database from backup archive",
	Long: `Restore database from a tar.gz backup archive created by the backup command.

The entire restore runs inside a single transaction: the archive is fully
validated before any data is touched, and any error rolls everything back and
exits non-zero, leaving the database unchanged.

Example:
  # Restore from a backup file
  image-bed restore --input ./backups/backup_20260214_222320.tar.gz

  # Preview only (validate the archive, write nothing)
  image-bed restore --input ./backup.tar.gz --dry-run

  # Restore specific tables only
  image-bed restore --input ./backup.tar.gz --tables users,images

  # Clear existing data first, without an interactive prompt
  image-bed restore --input ./backup.tar.gz --truncate --yes`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := initCommandLogger(); err != nil {
			exitWithErrorf("Failed to initialize config/logger: %v", err)
		}

		inputFile, _ := cmd.Flags().GetString("input")
		tables, _ := cmd.Flags().GetStringSlice("tables")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		truncate, _ := cmd.Flags().GetBool("truncate")
		assumeYes, _ := cmd.Flags().GetBool("yes")

		if err := runRestore(inputFile, tables, dryRun, truncate, assumeYes); err != nil {
			exitWithErrorf("Restore failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringP("input", "i", "", "Input tar.gz backup file path (required)")
	restoreCmd.Flags().StringSliceP("tables", "t", []string{}, "Specific tables to restore (default: all in archive)")
	restoreCmd.Flags().Bool("dry-run", false, "Validate the archive and preview the plan without writing to the database")
	restoreCmd.Flags().Bool("truncate", false, "Clear existing data before restore")
	restoreCmd.Flags().Bool("yes", false, "Skip the interactive confirmation prompt (for scripted restores)")

	_ = restoreCmd.MarkFlagRequired("input")
}

// restoreStats summarises a restore run.
type restoreStats struct {
	Restored       map[string]int64
	Truncated      bool
	SequencesReset int
}

func newRestoreStats() *restoreStats {
	return &restoreStats{Restored: make(map[string]int64)}
}

// runRestore orchestrates the CLI flow. Order is deliberate: extract, validate
// metadata, resolve the table selection, stream-validate every record, show the
// plan, confirm, and only then execute the transaction. All validation happens
// before the prompt and before the first database mutation.
func runRestore(inputFile string, tables []string, dryRun, truncate, assumeYes bool) error {
	if _, err := os.Stat(inputFile); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	cfg := config.Get()

	db, err := initDB()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	tempDir, err := os.MkdirTemp("", "image-bed-restore-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	restoreLog.Infof("Extracting backup: %s", inputFile)
	if err := extractTarGz(inputFile, tempDir); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	meta, err := loadAndValidateMetadata(tempDir)
	if err != nil {
		return err
	}
	restoreLog.Infof("Backup version: %s, Database: %s, Timestamp: %s",
		meta.Version, meta.Database, meta.Timestamp.Format("2006-01-02 15:04:05"))

	// fullRestore is true only when both: (1) user did NOT pass --tables, AND
	// (2) the archive itself contains all BackupData tables. A partial backup
	// (created with backup --tables) never triggers ephemeral-table clearing.
	fullRestore := len(tables) == 0 && archiveIsComplete(meta)
	selected, err := resolveRestoreTables(meta, tables)
	if err != nil {
		return err
	}

	// Full archive/record validation before any prompt or mutation.
	if err := validateArchiveData(tempDir, meta, selected); err != nil {
		return err
	}

	printRestorePlan(meta, selected, truncate, dryRun)

	if meta.Version == "1.0" {
		fmt.Println("\n⚠️  WARNING: This is a v1.0 (legacy) archive.")
		fmt.Println("Historical backup serialization used API-facing JSON models and may have")
		fmt.Println("omitted security-sensitive fields such as password hashes, encrypted configs,")
		fmt.Println("soft-delete state, and internal flags. This restore cannot guarantee a fully")
		fmt.Println("equivalent system. For complete disaster recovery, re-export with a current build.")
	}

	if dryRun {
		fmt.Println("\n[dry-run] Archive validated. No changes were written.")
		return nil
	}

	if !assumeYes {
		fmt.Println("\nWarning: This will restore data from the backup into the current database.")
		if truncate {
			fmt.Println("Existing data in the selected tables will be TRUNCATED.")
		}
		fmt.Print("Do you want to continue? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Restore cancelled.")
			return nil
		}
	}

	stats, err := executeRestore(db, cfg.DBType, tempDir, meta, selected, fullRestore, truncate)
	if err != nil {
		return err
	}

	printRestoreSummary(stats)
	return nil
}

// loadAndValidateMetadata reads and structurally validates metadata.json.
func loadAndValidateMetadata(dir string) (*backupMetadata, error) {
	file, err := os.Open(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}
	defer func() { _ = file.Close() }()

	var meta backupMetadata
	if err := json.NewDecoder(file).Decode(&meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if meta.Version != supportedArchiveVersion {
		return nil, fmt.Errorf("unsupported archive version %q (this build supports %q)", meta.Version, supportedArchiveVersion)
	}
	if len(meta.Tables) == 0 {
		return nil, errors.New("archive metadata lists no tables")
	}
	seen := make(map[string]bool, len(meta.Tables))
	for _, t := range meta.Tables {
		if seen[t] {
			return nil, fmt.Errorf("duplicate table %q in archive metadata", t)
		}
		seen[t] = true
	}
	for t, c := range meta.RecordCount {
		if c < 0 {
			return nil, fmt.Errorf("negative record count for table %q", t)
		}
	}
	return &meta, nil
}

// resolveRestoreTables determines which tables to restore, in manifest
// dependency order. A default (no --tables) restore uses every backed-up data
// table present in the archive. An explicit selection must be a subset of both
// the archive and the manifest's backup-data tables.
func resolveRestoreTables(meta *backupMetadata, requested []string) ([]string, error) {
	inArchive := make(map[string]bool, len(meta.Tables))
	for _, t := range meta.Tables {
		inArchive[t] = true
	}

	if len(requested) == 0 {
		// For v1.0 archives (fixed format), unknown tables indicate a corrupted
		// or incompatible archive and must fail rather than silently skip data.
		for _, t := range meta.Tables {
			spec, ok := database.TableByName(t)
			if !ok {
				return nil, fmt.Errorf("archive contains unknown table %q (not in manifest); v1.0 archives must match the current manifest exactly", t)
			}
			if !spec.BackupData {
				return nil, fmt.Errorf("archive table %q is not a restorable data table", t)
			}
		}
		var selected []string
		for _, spec := range database.RestoreOrder(meta.Tables) {
			selected = append(selected, spec.Name)
		}
		if len(selected) == 0 {
			return nil, errors.New("archive contains no restorable tables")
		}
		return selected, nil
	}

	for _, t := range requested {
		spec, ok := database.TableByName(t)
		if !ok || !spec.BackupData {
			return nil, fmt.Errorf("table %q is not a restorable data table", t)
		}
		if !inArchive[t] {
			return nil, fmt.Errorf("table %q is not present in the archive", t)
		}
	}
	var selected []string
	for _, spec := range database.RestoreOrder(requested) {
		selected = append(selected, spec.Name)
	}
	return selected, nil
}

// validateArchiveData streams every selected JSONL file, decoding each record
// into the manifest recovery type and confirming the line count matches the
// metadata. It holds no more than one line in memory at a time.
func validateArchiveData(dir string, meta *backupMetadata, selected []string) error {
	for _, table := range selected {
		spec, ok := database.TableByName(table)
		if !ok {
			return fmt.Errorf("table %q is not in the durable-table manifest", table)
		}
		expected, ok := meta.RecordCount[table]
		if !ok {
			return fmt.Errorf("archive metadata is missing a record count for %q", table)
		}
		path := filepath.Join(dir, table+".jsonl")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("archive is missing the data file for %q: %w", table, err)
		}
		count, err := streamJSONL(path, func(lineNum int, data []byte) error {
			rec := spec.NewRecord()
			if err := json.Unmarshal(data, rec); err != nil {
				return fmt.Errorf("table %s: invalid record at line %d: %w", table, lineNum, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if count != expected {
			return fmt.Errorf("table %s: archive contains %d records but metadata expects %d", table, count, expected)
		}
	}
	return nil
}

// executeRestore performs the entire restore within one transaction. Any error
// rolls the transaction back, leaving the database untouched.
func executeRestore(db *gorm.DB, dbType, dir string, meta *backupMetadata, selected []string, fullRestore, truncate bool) (*restoreStats, error) {
	stats := newRestoreStats()
	stats.Truncated = truncate

	preCounts := make(map[string]int64, len(selected))

	err := db.Transaction(func(tx *gorm.DB) error {
		if truncate {
			for _, name := range database.TruncateOrder(selected, fullRestore) {
				if err := tx.Table(name).Where("1 = 1").Delete(nil).Error; err != nil {
					return fmt.Errorf("failed to truncate %s: %w", name, err)
				}
			}
		} else {
			for _, table := range selected {
				n, err := physicalCount(tx, table)
				if err != nil {
					return err
				}
				preCounts[table] = n
			}
		}

		for _, spec := range database.RestoreOrder(selected) {
			affected, err := insertTable(tx, spec, dir)
			if err != nil {
				return fmt.Errorf("failed to restore %s: %w", spec.Name, err)
			}
			stats.Restored[spec.Name] = affected
		}

		reset, err := resetSequences(tx, dbType, selected)
		if err != nil {
			return err
		}
		stats.SequencesReset = reset

		// Verify the resulting row counts against the archive.
		for _, table := range selected {
			expected := meta.RecordCount[table]
			final, err := physicalCount(tx, table)
			if err != nil {
				return err
			}
			if truncate {
				if final != expected {
					return fmt.Errorf("table %s: expected %d rows after restore, found %d", table, expected, final)
				}
				continue
			}
			affected := stats.Restored[table]
			if affected != expected {
				return fmt.Errorf("table %s: inserted %d rows, expected %d", table, affected, expected)
			}
			if delta := final - preCounts[table]; delta != expected {
				return fmt.Errorf("table %s: row count grew by %d, expected %d", table, delta, expected)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// insertTable streams a table's JSONL file and inserts it in bounded batches,
// requiring each batch to affect exactly as many rows as were submitted.
func insertTable(tx *gorm.DB, spec database.TableSpec, dir string) (int64, error) {
	elemType := reflect.TypeOf(spec.NewRecord()).Elem() // e.g. models.User
	ptrSliceType := reflect.SliceOf(reflect.PointerTo(elemType))

	batch := reflect.MakeSlice(ptrSliceType, 0, restoreBatchSize)
	var affected int64

	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		holder := reflect.New(ptrSliceType)
		holder.Elem().Set(batch)

		var res *gorm.DB
		if spec.IsJoinTable {
			// Composite-key join table: insert exactly, no model, no conflict
			// handling. A duplicate key during a merge restore is a real error.
			res = tx.Table(spec.Name).CreateInBatches(holder.Interface(), batch.Len())
		} else {
			// Insert only the row's own columns; never cascade into associations.
			res = tx.Omit(clause.Associations).CreateInBatches(holder.Interface(), batch.Len())
		}
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(batch.Len()) {
			return fmt.Errorf("batch inserted %d of %d rows", res.RowsAffected, batch.Len())
		}
		affected += res.RowsAffected
		batch = reflect.MakeSlice(ptrSliceType, 0, restoreBatchSize)
		return nil
	}

	path := filepath.Join(dir, spec.Name+".jsonl")
	_, err := streamJSONL(path, func(lineNum int, data []byte) error {
		recPtr := reflect.New(elemType)
		if err := json.Unmarshal(data, recPtr.Interface()); err != nil {
			return fmt.Errorf("invalid record at line %d: %w", lineNum, err)
		}
		batch = reflect.Append(batch, recPtr)
		if batch.Len() >= restoreBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return affected, err
	}
	if err := flush(); err != nil {
		return affected, err
	}
	return affected, nil
}

// resetSequences resets the auto-increment sequence of each restored table so
// the next inserted row gets an id above the restored maximum. Sequence errors
// fail the restore.
func resetSequences(tx *gorm.DB, dbType string, selected []string) (int, error) {
	specs := database.AutoIDTables(selected)
	if len(specs) == 0 {
		return 0, nil
	}
	switch dbType {
	case "sqlite", "sqlite3":
		return resetSQLiteSequences(tx, specs)
	case "postgres", "postgresql":
		return resetPostgresSequences(tx, specs)
	default:
		return 0, nil
	}
}

// resetSQLiteSequences resets the AUTOINCREMENT counters. If the schema uses
// plain rowid primary keys (no AUTOINCREMENT, hence no sqlite_sequence table),
// there is nothing to reset: SQLite already derives the next id from max+1.
func resetSQLiteSequences(tx *gorm.DB, specs []database.TableSpec) (int, error) {
	var hasSeqTable int64
	if err := tx.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'",
	).Scan(&hasSeqTable).Error; err != nil {
		return 0, fmt.Errorf("failed to inspect sqlite_sequence: %w", err)
	}
	if hasSeqTable == 0 {
		return 0, nil
	}

	reset := 0
	for _, spec := range specs {
		maxID, err := maxTableID(tx, spec.Name)
		if err != nil {
			return reset, err
		}
		// Deterministic: clear any existing counter, then set it only when the
		// table has rows. An empty table is left without a counter so the next
		// insert starts at 1.
		if err := tx.Exec("DELETE FROM sqlite_sequence WHERE name = ?", spec.Name).Error; err != nil {
			return reset, fmt.Errorf("failed to reset sequence for %s: %w", spec.Name, err)
		}
		if maxID > 0 {
			if err := tx.Exec("INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)", spec.Name, maxID).Error; err != nil {
				return reset, fmt.Errorf("failed to set sequence for %s: %w", spec.Name, err)
			}
		}
		reset++
	}
	return reset, nil
}

// resetPostgresSequences uses transactional ALTER SEQUENCE ... RESTART to
// reset each table's serial sequence. Unlike setval(), RESTART participates in
// transaction rollback. Sequence names are resolved via pg_get_serial_sequence,
// which is safe because table names come from the validated manifest.
func resetPostgresSequences(tx *gorm.DB, specs []database.TableSpec) (int, error) {
	reset := 0
	for _, spec := range specs {
		maxID, err := maxTableID(tx, spec.Name)
		if err != nil {
			return reset, err
		}
		var seqName *string
		if err := tx.Raw("SELECT pg_get_serial_sequence(?, 'id')", spec.Name).Scan(&seqName).Error; err != nil {
			return reset, fmt.Errorf("failed to resolve sequence for %s: %w", spec.Name, err)
		}
		if seqName == nil || *seqName == "" {
			continue // table has no serial-backed id sequence; nothing to reset
		}
		// Use identifier quoting to safely embed the resolved sequence name.
		// ALTER SEQUENCE is transactional: it will roll back with the restore.
		if maxID > 0 {
			if err := tx.Exec(fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH %d", *seqName, maxID+1)).Error; err != nil {
				return reset, fmt.Errorf("failed to restart sequence for %s: %w", spec.Name, err)
			}
		} else {
			if err := tx.Exec(fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH 1", *seqName)).Error; err != nil {
				return reset, fmt.Errorf("failed to restart sequence for %s: %w", spec.Name, err)
			}
		}
		reset++
	}
	return reset, nil
}

func maxTableID(tx *gorm.DB, table string) (int64, error) {
	var maxID int64
	if err := tx.Table(table).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, fmt.Errorf("failed to read max id for %s: %w", table, err)
	}
	return maxID, nil
}

// physicalCount returns the physical row count of a table (including
// soft-deleted rows, since Table() applies no soft-delete scope).
func physicalCount(tx *gorm.DB, table string) (int64, error) {
	var n int64
	if err := tx.Table(table).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("failed to count %s: %w", table, err)
	}
	return n, nil
}

// streamJSONL reads a JSONL file line by line, rejecting blank lines and lines
// over maxJSONLLineBytes, invoking handle for each non-empty line. It never
// retains more than a single line. It returns the number of records.
func streamJSONL(path string, handle func(lineNum int, data []byte) error) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxJSONLLineBytes)

	var count int64
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return count, fmt.Errorf("%s: unexpected blank line at line %d", filepath.Base(path), lineNum)
		}
		buf := make([]byte, len(line))
		copy(buf, line)
		if err := handle(lineNum, buf); err != nil {
			return count, err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return count, fmt.Errorf("%s: record exceeds the %d byte line limit", filepath.Base(path), maxJSONLLineBytes)
		}
		return count, fmt.Errorf("%s: read error: %w", filepath.Base(path), err)
	}
	return count, nil
}

// extractTarGz extracts a backup archive into destDir. It accepts only
// top-level regular files and enforces entry-count, per-file, and total-size
// limits to defend against path traversal and archive-expansion attacks.
func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)
	seen := make(map[string]bool)
	var total int64
	entries := 0

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("archive has too many entries (>%d)", maxArchiveEntries)
		}

		name := header.Name
		// Legacy v1 archives written by createTarGz included the source root as
		// a "." directory entry. It carries no data, so accept and ignore only
		// this exact directory while continuing to reject all other non-files.
		if name == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive contains a non-regular entry %q (type %d)", name, header.Typeflag)
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("illegal entry name in archive: %q", name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate entry in archive: %q", name)
		}
		seen[name] = true
		if header.Size > maxArchiveFileBytes {
			return fmt.Errorf("archive entry %q is too large", name)
		}

		target := filepath.Join(destDir, name)
		outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		written, err := io.Copy(outFile, io.LimitReader(tarReader, maxArchiveFileBytes+1))
		_ = outFile.Close()
		if err != nil {
			return err
		}
		if written > maxArchiveFileBytes {
			return fmt.Errorf("archive entry %q exceeds the size limit", name)
		}
		total += written
		if total > maxArchiveTotalBytes {
			return fmt.Errorf("archive expands beyond the total size limit (%d bytes)", maxArchiveTotalBytes)
		}
	}
	return nil
}

// archiveIsComplete returns true if meta.Tables includes every BackupData table
// from the manifest. Only complete archives should clear ephemeral tables.
func archiveIsComplete(meta *backupMetadata) bool {
	inArchive := make(map[string]bool, len(meta.Tables))
	for _, t := range meta.Tables {
		inArchive[t] = true
	}

	if meta.Version == "1.0" {
		for _, name := range legacyV1DefaultBackupTables {
			if !inArchive[name] {
				return false
			}
		}
		return true
	}

	for _, spec := range database.BackupTables() {
		if !inArchive[spec.Name] {
			return false
		}
	}
	return true
}

// printRestorePlan shows the validated plan before any prompt.
func printRestorePlan(meta *backupMetadata, selected []string, truncate, dryRun bool) {
	fmt.Println()
	fmt.Println("========================================")
	if dryRun {
		fmt.Println("       [DRY RUN] Restore Plan")
	} else {
		fmt.Println("            Restore Plan")
	}
	fmt.Println("========================================")
	fmt.Printf("Archive version: %s\n", meta.Version)
	fmt.Printf("Source database: %s\n", meta.Database)
	fmt.Printf("Truncate first:  %v\n", truncate)
	fmt.Println("Tables to restore:")
	for _, table := range selected {
		fmt.Printf("  %-22s %d records\n", table+":", meta.RecordCount[table])
	}
	fmt.Println("========================================")
}

// printRestoreSummary prints the result of a completed restore.
func printRestoreSummary(stats *restoreStats) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("           Restore Summary")
	fmt.Println("========================================")
	if stats.Truncated {
		fmt.Println("Existing data was truncated before restore.")
	}
	fmt.Println("Restored records:")
	for table, count := range stats.Restored {
		fmt.Printf("  %-22s %d\n", table+":", count)
	}
	fmt.Printf("\nAuto-increment sequences reset: %d\n", stats.SequencesReset)
	fmt.Println("========================================")
}
