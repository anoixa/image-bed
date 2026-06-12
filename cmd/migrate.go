package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/utils"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var migrateLog = utils.ForModule("Migrate")

// migrateCmd 数据库迁移命令
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration tools",
	Long:  `Migrate data from one database to another (e.g., SQLite to PostgreSQL).`,
}

// migrateRunCmd 执行迁移命令
var migrateRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run database migration",
	Long: `Run database migration from source to target database.

Examples:
  # Migrate from SQLite to PostgreSQL
  image-bed migrate run --from-sqlite ./data.db --to-postgres "host=localhost user=postgres password=secret dbname=imagebed port=5432"

  # Migrate with overwrite strategy (replace existing data)
  image-bed migrate run --from-sqlite ./data.db --to-postgres "..." --on-conflict=overwrite

  # Stop on conflict
  image-bed migrate run --from-sqlite ./data.db --to-postgres "..." --on-conflict=error`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := initCommandLogger(); err != nil {
			exitWithErrorf("Failed to initialize config/logger: %v", err)
		}

		fromType, _ := cmd.Flags().GetString("from-type")
		toType, _ := cmd.Flags().GetString("to-type")
		fromDSN, _ := cmd.Flags().GetString("from-dsn")
		toDSN, _ := cmd.Flags().GetString("to-dsn")
		fromSQLite, _ := cmd.Flags().GetString("from-sqlite")
		toPostgres, _ := cmd.Flags().GetString("to-postgres")
		skipConfirm, _ := cmd.Flags().GetBool("yes")
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		onConflict, _ := cmd.Flags().GetString("on-conflict")

		if err := runMigration(fromType, toType, fromDSN, toDSN, fromSQLite, toPostgres, skipConfirm, batchSize, onConflict); err != nil {
			exitWithErrorf("Migration failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(migrateRunCmd)

	migrateRunCmd.Flags().String("from-type", "", "Source database type (sqlite, postgres, mysql)")
	migrateRunCmd.Flags().String("to-type", "", "Target database type (sqlite, postgres, mysql)")
	migrateRunCmd.Flags().String("from-dsn", "", "Source database DSN/connection string")
	migrateRunCmd.Flags().String("to-dsn", "", "Target database DSN/connection string")
	migrateRunCmd.Flags().String("from-sqlite", "", "Source SQLite file path (shortcut)")
	migrateRunCmd.Flags().String("to-postgres", "", "Target PostgreSQL connection string (shortcut)")
	migrateRunCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	migrateRunCmd.Flags().Int("batch-size", 100, "Batch size for data migration")
	migrateRunCmd.Flags().String("on-conflict", "skip", "Conflict resolution strategy: skip (default), overwrite, error")
}

// migrateStats 迁移统计
type migrateStats struct {
	migrated    map[string]int // per-table inserted/overwritten row count
	sourceRows  map[string]int // rows read from each source table
	processed   map[string]int // rows successfully inserted, overwritten, or skipped
	present     map[string]bool
	skipped     int // skipped on conflict
	overwritten int // overwritten on conflict
}

func newMigrateStats() *migrateStats {
	return &migrateStats{
		migrated:   make(map[string]int),
		sourceRows: make(map[string]int),
		processed:  make(map[string]int),
		present:    make(map[string]bool),
	}
}

// runMigration 执行数据库迁移
func runMigration(fromType, toType, fromDSN, toDSN, fromSQLite, toPostgres string, skipConfirm bool, batchSize int, onConflict string) error {
	if onConflict != "skip" && onConflict != "overwrite" && onConflict != "error" {
		return fmt.Errorf("invalid on-conflict strategy: %s (must be skip, overwrite, or error)", onConflict)
	}

	if fromSQLite != "" {
		fromType = "sqlite"
		fromDSN = fromSQLite
	}
	if toPostgres != "" {
		toType = "postgres"
		toDSN = toPostgres
	}

	if fromType == "" || toType == "" {
		return fmt.Errorf("both --from-type and --to-type are required")
	}
	if fromDSN == "" || toDSN == "" {
		return fmt.Errorf("both --from-dsn and --to-dsn (or shortcuts) are required")
	}

	if fromType == toType && fromDSN == toDSN {
		return fmt.Errorf("source and target databases are the same")
	}

	if batchSize <= 0 {
		batchSize = 100
	}

	migrateLog.Infof("Migrating from %s to %s", fromType, toType)
	migrateLog.Infof("Source: %s", maskDSN(fromDSN))
	migrateLog.Infof("Target: %s", maskDSN(toDSN))
	migrateLog.Infof("Conflict strategy: %s", onConflict)

	sourceDB, err := openDatabase(fromType, fromDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to source database: %w", err)
	}
	sqlDB, _ := sourceDB.DB()
	defer func() { _ = sqlDB.Close() }()

	targetDB, err := openDatabase(toType, toDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}
	sqlDB2, _ := targetDB.DB()
	defer func() { _ = sqlDB2.Close() }()

	// 确认迁移
	if !skipConfirm {
		fmt.Println("\nWarning: This will migrate all data from source to target database.")
		fmt.Printf("Conflict resolution strategy: %s\n", onConflict)
		fmt.Println("Existing data in target database may be affected.")
		fmt.Print("Do you want to continue? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	// Migrate every table the manifest marks as MigrateData, in dependency
	// order. Driving the loop from the manifest (rather than a hardcoded list)
	// is what keeps the migration table set from drifting out of sync with the
	// schema — the historical cause of system_configs / user_identities /
	// user_totp_settings silently never being migrated.
	stats := newMigrateStats()
	ctx := context.Background()
	sourceTxOptions := &sql.TxOptions{ReadOnly: true}
	if fromType == "postgres" || fromType == "postgresql" {
		sourceTxOptions.Isolation = sql.LevelRepeatableRead
	} else {
		sourceTxOptions.Isolation = sql.LevelSerializable
	}
	err = sourceDB.Transaction(func(sourceTx *gorm.DB) error {
		return targetDB.Transaction(func(targetTx *gorm.DB) error {
			migrateLog.Infof("Migrating database schema")
			if err := autoMigrate(targetTx); err != nil {
				return fmt.Errorf("failed to migrate schema: %w", err)
			}

			var migratedTables []string
			for _, spec := range database.MigrateTables() {
				migrateLog.Infof("Migrating %s", spec.Name)
				if err := migrateTable(ctx, sourceTx, targetTx, spec, stats, batchSize, onConflict); err != nil {
					return fmt.Errorf("%s migration failed: %w", spec.Name, err)
				}
				migratedTables = append(migratedTables, spec.Name)
			}

			if err := validateMigrationCounts(targetTx, stats); err != nil {
				return err
			}

			// Rows are inserted with explicit ids, so target sequences must be reset
			// inside the same transaction as the data they describe.
			reset, err := resetSequences(targetTx, toType, migratedTables)
			if err != nil {
				return fmt.Errorf("sequence reset failed: %w", err)
			}
			if reset > 0 {
				migrateLog.Infof("Reset %d auto-increment sequence(s) on the target", reset)
			}
			return nil
		})
	}, sourceTxOptions)
	if err != nil {
		return err
	}

	printMigrateStats(stats)

	migrateLog.Infof("Migration completed successfully")
	return nil
}

// openDatabase 打开数据库连接
func openDatabase(dbType, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch dbType {
	case "sqlite":
		sqliteDSN := dsn
		if sqliteDSN == "" {
			sqliteDSN = "file::memory:?cache=shared"
		}
		dialector = sqlite.Open(sqliteDSN)
	case "postgres", "postgresql":
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// autoMigrate 自动迁移数据库结构
func autoMigrate(db *gorm.DB) error {
	// Source the schema models from the shared durable-table manifest so the
	// migration target schema can never drift from the application schema.
	return db.AutoMigrate(database.MigrationModels()...)
}

// migrateTable copies one manifest table from source to target. A table absent
// from the source is skipped (a legitimate "older schema" case); any OTHER read
// error is propagated rather than being silently swallowed as "table missing"
// (the historical F5 bug in migrateAlbumImages / migrateTokens).
func migrateTable(ctx context.Context, source, target *gorm.DB, spec database.TableSpec, stats *migrateStats, batchSize int, onConflict string) error {
	if spec.IsJoinTable {
		return migrateJoinTable(ctx, source, target, spec, stats, onConflict)
	}

	elemType := reflect.TypeOf(spec.NewRecord()).Elem() // e.g. models.User
	sliceType := reflect.SliceOf(elemType)

	offset := 0
	for {
		slicePtr := reflect.New(sliceType)
		// Unscoped: the models embed gorm.DeletedAt, so a plain read would have
		// GORM apply the soft-delete scope (WHERE deleted_at IS NULL) and silently
		// drop soft-deleted rows. Read the physical table so migration matches the
		// backup/restore contract.
		err := source.WithContext(ctx).Unscoped().Table(spec.Name).
			Order("id").
			Limit(batchSize).Offset(offset).
			Find(slicePtr.Interface()).Error
		if err != nil {
			// A table absent from the source (an older schema) is a legitimate
			// skip; any other error (connection, permissions, ...) must surface
			// rather than be silently treated as "table missing".
			if offset == 0 && isMissingTableError(err) {
				migrateLog.Infof("Source database has no %q table; skipping", spec.Name)
				return nil
			}
			return err
		}

		rows := slicePtr.Elem()
		n := rows.Len()
		stats.present[spec.Name] = true
		if n == 0 {
			break
		}
		stats.sourceRows[spec.Name] += n

		for i := range n {
			row := rows.Index(i)
			id, ok := recordID(row)
			if !ok {
				return fmt.Errorf("record in %q has no usable id", spec.Name)
			}
			recPtr := row.Addr().Interface()

			create, overwrite, err := resolveRowConflict(ctx, target, spec.Name, id, onConflict)
			if err != nil {
				return fmt.Errorf("conflict check failed for %s id=%d: %w", spec.Name, id, err)
			}

			switch {
			case overwrite:
				if err := target.WithContext(ctx).Table(spec.Name).Where("id = ?", id).Delete(nil).Error; err != nil {
					return fmt.Errorf("failed to delete existing %s id=%d: %w", spec.Name, id, err)
				}
				if err := target.WithContext(ctx).Omit(clause.Associations).Create(recPtr).Error; err != nil {
					return fmt.Errorf("failed to overwrite %s id=%d: %w", spec.Name, id, err)
				}
				stats.overwritten++
				stats.migrated[spec.Name]++
			case create:
				if err := target.WithContext(ctx).Omit(clause.Associations).Create(recPtr).Error; err != nil {
					return fmt.Errorf("failed to migrate %s id=%d: %w", spec.Name, id, err)
				}
				stats.migrated[spec.Name]++
			default:
				stats.skipped++
			}
			stats.processed[spec.Name]++
		}

		offset += batchSize
	}

	migrateLog.Infof("Migrated %d %s", stats.migrated[spec.Name], spec.Name)
	return nil
}

// resolveRowConflict decides whether a row with the given id should be created,
// overwritten, or skipped on the target. It counts the physical row (no
// soft-delete scope) so a soft-deleted target row still counts as a conflict.
func resolveRowConflict(ctx context.Context, target *gorm.DB, table string, id uint, onConflict string) (create, overwrite bool, err error) {
	var count int64
	if err := target.WithContext(ctx).Table(table).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, false, err
	}
	if count == 0 {
		return true, false, nil
	}
	switch onConflict {
	case "overwrite":
		return false, true, nil
	case "error":
		return false, false, fmt.Errorf("record already exists in %s: id=%d", table, id)
	default: // skip
		return false, false, nil
	}
}

// migrateJoinTable copies the composite-key album_images join. It inserts only
// relations whose referenced album and image exist on the target, and treats an
// existing relation as a conflict (error strategy) or a skip (otherwise).
func migrateJoinTable(ctx context.Context, source, target *gorm.DB, spec database.TableSpec, stats *migrateStats, onConflict string) error {
	type albumImage struct {
		AlbumID uint
		ImageID uint
	}

	var relations []albumImage
	if err := source.WithContext(ctx).Raw("SELECT album_id, image_id FROM album_images").Scan(&relations).Error; err != nil {
		if isMissingTableError(err) {
			migrateLog.Infof("Source database has no %q table; skipping", spec.Name)
			return nil
		}
		return err
	}
	stats.present[spec.Name] = true
	stats.sourceRows[spec.Name] = len(relations)

	for _, rel := range relations {
		var existing int64
		if err := target.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM album_images WHERE album_id = ? AND image_id = ?",
			rel.AlbumID, rel.ImageID,
		).Scan(&existing).Error; err != nil {
			return fmt.Errorf("failed to check existing relation (album=%d, image=%d): %w", rel.AlbumID, rel.ImageID, err)
		}
		if existing > 0 {
			if onConflict == "error" {
				return fmt.Errorf("album_image relation already exists: album_id=%d, image_id=%d", rel.AlbumID, rel.ImageID)
			}
			stats.skipped++
			stats.processed[spec.Name]++
			continue
		}

		var albumCount, imageCount int64
		if err := target.WithContext(ctx).Table("albums").Where("id = ?", rel.AlbumID).Count(&albumCount).Error; err != nil {
			return fmt.Errorf("failed to check album existence (id=%d): %w", rel.AlbumID, err)
		}
		if err := target.WithContext(ctx).Table("images").Where("id = ?", rel.ImageID).Count(&imageCount).Error; err != nil {
			return fmt.Errorf("failed to check image existence (id=%d): %w", rel.ImageID, err)
		}
		if albumCount == 0 || imageCount == 0 {
			return fmt.Errorf("album_image relation references missing target row: album_id=%d, image_id=%d", rel.AlbumID, rel.ImageID)
		}

		if err := target.WithContext(ctx).Exec(
			"INSERT INTO album_images (album_id, image_id) VALUES (?, ?)",
			rel.AlbumID, rel.ImageID,
		).Error; err != nil {
			return fmt.Errorf("failed to migrate album_image relation (album=%d, image=%d): %w", rel.AlbumID, rel.ImageID, err)
		}
		stats.migrated[spec.Name]++
		stats.processed[spec.Name]++
	}

	migrateLog.Infof("Migrated %d album_image relations", stats.migrated[spec.Name])
	return nil
}

// isMissingTableError reports whether err indicates the queried table does not
// exist on the source. It deliberately matches only SQLite's precise message
// and PostgreSQL's undefined_table SQLSTATE; generic "does not exist" text can
// also describe missing columns, functions, schemas, or permissions.
func isMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "42P01" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table:")
}

// validateMigrationCounts catches silent row loss before commit. Existing
// target rows are allowed by the skip/overwrite strategies, so target counts
// may exceed source counts but may never be lower.
func validateMigrationCounts(target *gorm.DB, stats *migrateStats) error {
	for _, spec := range database.MigrateTables() {
		if !stats.present[spec.Name] {
			continue
		}
		sourceCount := stats.sourceRows[spec.Name]
		if stats.processed[spec.Name] != sourceCount {
			return fmt.Errorf("table %s: processed %d of %d source rows", spec.Name, stats.processed[spec.Name], sourceCount)
		}
		var targetCount int64
		if err := target.Table(spec.Name).Count(&targetCount).Error; err != nil {
			return fmt.Errorf("failed to validate target count for %s: %w", spec.Name, err)
		}
		if targetCount < int64(sourceCount) {
			return fmt.Errorf("table %s: target has %d rows after migration, fewer than %d source rows", spec.Name, targetCount, sourceCount)
		}
	}
	return nil
}

// recordID reads the uint primary key "ID" field from a record value.
func recordID(v reflect.Value) (uint, bool) {
	f := v.FieldByName("ID")
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uint(f.Uint()), true
	default:
		return 0, false
	}
}

// maskDSN 隐藏敏感信息
func maskDSN(dsn string) string {
	if len(dsn) > 50 {
		return dsn[:50] + "..."
	}
	return dsn
}

// printMigrateStats 打印迁移统计
func printMigrateStats(stats *migrateStats) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("       Migration Statistics")
	fmt.Println("========================================")
	var total int
	for _, spec := range database.MigrateTables() {
		n := stats.migrated[spec.Name]
		total += n
		fmt.Printf("%-22s %d\n", spec.Name+":", n)
	}
	fmt.Println("----------------------------------------")
	fmt.Printf("%-22s %d\n", "Total migrated:", total)
	fmt.Printf("%-22s %d\n", "Skipped records:", stats.skipped)
	fmt.Printf("%-22s %d\n", "Overwritten:", stats.overwritten)
	fmt.Println("========================================")

}
