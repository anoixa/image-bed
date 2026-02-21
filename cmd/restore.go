package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// restoreCmd 数据库还原命令
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore database from backup archive",
	Long: `Restore database from tar.gz backup archive created by backup command.

Example:
  # Restore from backup file
  image-bed restore --input ./backups/backup_20260214_222320.tar.gz

  # Restore with dry-run (preview only)
  image-bed restore --input ./backup.tar.gz --dry-run

  # Restore specific tables only
  image-bed restore --input ./backup.tar.gz --tables users,images

  # Clear existing data before restore
  image-bed restore --input ./backup.tar.gz --truncate`,
	Run: func(cmd *cobra.Command, args []string) {
		inputFile, _ := cmd.Flags().GetString("input")
		tables, _ := cmd.Flags().GetStringSlice("tables")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		truncate, _ := cmd.Flags().GetBool("truncate")

		if err := runRestore(inputFile, tables, dryRun, truncate); err != nil {
			log.Fatalf("Restore failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringP("input", "i", "", "Input tar.gz backup file path (required)")
	restoreCmd.Flags().StringSliceP("tables", "t", []string{}, "Specific tables to restore (default: all)")
	restoreCmd.Flags().Bool("dry-run", false, "Preview restore without actually writing to database")
	restoreCmd.Flags().Bool("truncate", false, "Clear existing data before restore")

	_ = restoreCmd.MarkFlagRequired("input")
}


// restoreStats 还原统计
type restoreStats struct {
	Restored             map[string]int64
	Skipped              map[string]int64
	Errors               map[string]int64
	AutoIncrementUpdates int
}

func newRestoreStats() *restoreStats {
	return &restoreStats{
		Restored: make(map[string]int64),
		Skipped:  make(map[string]int64),
		Errors:   make(map[string]int64),
	}
}

// runRestore 执行还原
func runRestore(inputFile string, tables []string, dryRun, truncate bool) error {
	// 验证输入文件
	if _, err := os.Stat(inputFile); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	config.InitConfig()
	cfg := config.Get()

	db, err := initDB()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	// 创建临时目录解压备份
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("image-bed-restore-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	log.Printf("Extracting backup: %s", inputFile)

	// 解压 tar.gz
	if err := extractTarGz(inputFile, tempDir); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	// 读取元数据
	metadataPath := filepath.Join(tempDir, "metadata.json")
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	log.Printf("Backup version: %s, Database: %s, Timestamp: %s",
		metadata.Version, metadata.Database, metadata.Timestamp.Format("2006-01-02 15:04:05"))

	// 确定要还原的表
	if len(tables) == 0 {
		tables = metadata.Tables
	}

	// 确认还原
	if !dryRun {
		fmt.Println("\nWarning: This will restore data from backup to the current database.")
		if truncate {
			fmt.Println("Existing data will be TRUNCATED.")
		}
		fmt.Print("Do you want to continue? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Restore cancelled.")
			return nil
		}
	}

	stats := newRestoreStats()

	// 如果需要，清空现有数据（按依赖顺序逆序）
	if truncate && !dryRun {
		log.Println("Truncating existing data...")
		if err := truncateTables(db, tables); err != nil {
			return fmt.Errorf("failed to truncate tables: %w", err)
		}
	}

	// 按依赖顺序还原表
	// 顺序：users -> devices -> images -> albums -> album_images -> api_tokens
	restoreOrder := []string{"users", "devices", "images", "albums", "album_images", "api_tokens"}

	for _, table := range restoreOrder {
		if !contains(tables, table) {
			continue
		}

		jsonlPath := filepath.Join(tempDir, table+".jsonl")
		if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
			log.Printf("Skipping %s: file not found in backup", table)
			continue
		}

		log.Printf("Restoring table: %s", table)
		if err := restoreTable(db, table, jsonlPath, stats, dryRun); err != nil {
			log.Printf("Error restoring table %s: %v", table, err)
			stats.Errors[table] = 1
		}
	}

	// 更新自增序列
	if !dryRun {
		log.Println("Updating auto-increment sequences...")
		if err := updateAutoIncrementSequences(db, cfg.DBType, stats); err != nil {
			log.Printf("Warning: failed to update auto-increment sequences: %v", err)
		}
	}

	printRestoreSummary(stats, dryRun)

	return nil
}

// readMetadata 读取元数据
func readMetadata(path string) (*backupMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var metadata backupMetadata
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// extractTarGz 解压 tar.gz 文件
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

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				return err
			}
			_ = outFile.Close()
		}
	}

	return nil
}

// restoreTable 还原单张表（使用批量插入优化）
func restoreTable(db *gorm.DB, tableName, jsonlPath string, stats *restoreStats, dryRun bool) error {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	var lineNum int
	batchSize := 100 // 每批插入 100 条

	// 使用通用批量还原函数
	switch tableName {
	case "users":
		restoreBatch(db, scanner, &lineNum, batchSize, dryRun, stats, tableName,
			func() interface{} { return &models.User{} })
	case "devices":
		restoreBatch(db, scanner, &lineNum, batchSize, dryRun, stats, tableName,
			func() interface{} { return &models.Device{} })
	case "images":
		restoreBatchWithCleanup(db, scanner, &lineNum, batchSize, dryRun, stats, tableName,
			func() *models.Image { return &models.Image{} },
			func(r *models.Image) { r.Albums = nil })
	case "albums":
		restoreBatchWithCleanup(db, scanner, &lineNum, batchSize, dryRun, stats, tableName,
			func() *models.Album { return &models.Album{} },
			func(r *models.Album) { r.Images = nil })
	case "album_images":
		restoreAlbumImagesBatch(db, scanner, &lineNum, batchSize, dryRun, stats)
	case "api_tokens":
		restoreBatch(db, scanner, &lineNum, batchSize, dryRun, stats, tableName,
			func() interface{} { return &models.ApiToken{} })
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading JSONL: %w", err)
	}

	log.Printf("Restored %d records to %s", stats.Restored[tableName], tableName)
	return nil
}

// restoreBatch 通用批量还原
func restoreBatch(db *gorm.DB, scanner *bufio.Scanner, lineNum *int, batchSize int, dryRun bool, stats *restoreStats, tableName string, newRecord func() interface{}) {
	batch := make([]interface{}, 0, batchSize)

	for scanner.Scan() {
		*lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		record := newRecord()
		if err := json.Unmarshal([]byte(line), record); err != nil {
			log.Printf("Warning: failed to unmarshal %s record at line %d: %v", tableName, *lineNum, err)
			stats.Errors[tableName]++
			continue
		}
		batch = append(batch, record)

		if len(batch) >= batchSize {
			insertBatch(db, batch, dryRun, stats, tableName)
			batch = batch[:0]
		}
	}

	// 处理剩余记录
	if len(batch) > 0 {
		insertBatch(db, batch, dryRun, stats, tableName)
	}
}

// restoreBatchWithCleanup 带清理的批量还原
func restoreBatchWithCleanup[T any](db *gorm.DB, scanner *bufio.Scanner, lineNum *int, batchSize int, dryRun bool, stats *restoreStats, tableName string, newRecord func() *T, cleanup func(*T)) {
	batch := make([]*T, 0, batchSize)

	for scanner.Scan() {
		*lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		record := newRecord()
		if err := json.Unmarshal([]byte(line), record); err != nil {
			log.Printf("Warning: failed to unmarshal %s record at line %d: %v", tableName, *lineNum, err)
			stats.Errors[tableName]++
			continue
		}
		cleanup(record)
		batch = append(batch, record)

		if len(batch) >= batchSize {
			if !dryRun {
				if err := db.CreateInBatches(batch, batchSize).Error; err != nil {
					log.Printf("Warning: failed to insert batch: %v", err)
					stats.Errors[tableName] += int64(len(batch))
				} else {
					stats.Restored[tableName] += int64(len(batch))
				}
			} else {
				stats.Restored[tableName] += int64(len(batch))
			}
			batch = batch[:0]
		}
	}

	// 处理剩余记录
	if len(batch) > 0 && !dryRun {
		if err := db.CreateInBatches(batch, len(batch)).Error; err != nil {
			log.Printf("Warning: failed to insert final batch: %v", err)
			stats.Errors[tableName] += int64(len(batch))
		} else {
			stats.Restored[tableName] += int64(len(batch))
		}
	} else if len(batch) > 0 {
		stats.Restored[tableName] += int64(len(batch))
	}
}

// restoreAlbumImagesBatch 还原 album_images（使用原始 SQL）
func restoreAlbumImagesBatch(db *gorm.DB, scanner *bufio.Scanner, lineNum *int, batchSize int, dryRun bool, stats *restoreStats) {
	batch := make([]albumImageRecord, 0, batchSize)

	for scanner.Scan() {
		*lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var record albumImageRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			log.Printf("Warning: failed to unmarshal album_images record at line %d: %v", *lineNum, err)
			stats.Errors["album_images"]++
			continue
		}
		batch = append(batch, record)

		if len(batch) >= batchSize {
			insertAlbumImagesBatch(db, batch, dryRun, stats)
			batch = batch[:0]
		}
	}

	// 处理剩余记录
	if len(batch) > 0 {
		insertAlbumImagesBatch(db, batch, dryRun, stats)
	}
}

// insertBatch 插入批量记录（通用）
func insertBatch(db *gorm.DB, batch []interface{}, dryRun bool, stats *restoreStats, tableName string) {
	if dryRun {
		stats.Restored[tableName] += int64(len(batch))
		return
	}

	// 使用事务批量插入
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, record := range batch {
			if err := tx.Create(record).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Warning: failed to insert batch: %v", err)
		stats.Errors[tableName] += int64(len(batch))
	} else {
		stats.Restored[tableName] += int64(len(batch))
	}
}

// insertAlbumImagesBatch 批量插入 album_images
func insertAlbumImagesBatch(db *gorm.DB, batch []albumImageRecord, dryRun bool, stats *restoreStats) {
	if dryRun {
		stats.Restored["album_images"] += int64(len(batch))
		return
	}

	if len(batch) == 0 {
		return
	}

	// 构建批量插入 SQL
	var values []string
	var args []interface{}
	for _, record := range batch {
		values = append(values, "(?, ?)")
		args = append(args, record.AlbumID, record.ImageID)
	}

	sql := "INSERT OR IGNORE INTO album_images (album_id, image_id) VALUES " +
		strings.Join(values, ", ")
	if err := db.Exec(sql, args...).Error; err != nil {
		log.Printf("Warning: failed to insert album_images batch: %v", err)
		stats.Errors["album_images"] += int64(len(batch))
	} else {
		stats.Restored["album_images"] += int64(len(batch))
	}
}

// truncateTables 清空表数据
func truncateTables(db *gorm.DB, tables []string) error {
	// 按依赖逆序清空
	truncateOrder := []string{"album_images", "api_tokens", "images", "albums", "devices", "users"}

	for _, table := range truncateOrder {
		if !contains(tables, table) {
			continue
		}

		log.Printf("Truncating table: %s", table)

		// 针对不同表使用不同的清空方式
		switch table {
		case "album_images":
			if err := db.Exec("DELETE FROM album_images").Error; err != nil {
				return fmt.Errorf("failed to truncate %s: %w", table, err)
			}
		default:
			if err := db.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
				return fmt.Errorf("failed to truncate %s: %w", table, err)
			}
		}
	}

	return nil
}

// updateAutoIncrementSequences 更新自增序列
func updateAutoIncrementSequences(db *gorm.DB, dbType string, stats *restoreStats) error {
	type tableInfo struct {
		TableName string
		Model     interface{}
	}

	tables := []tableInfo{
		{"users", &models.User{}},
		{"devices", &models.Device{}},
		{"images", &models.Image{}},
		{"albums", &models.Album{}},
		{"api_tokens", &models.ApiToken{}},
	}

	for _, info := range tables {
		var maxID uint
		if err := db.Table(info.TableName).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
			log.Printf("Warning: failed to get max ID for %s: %v", info.TableName, err)
			continue
		}

		if maxID == 0 {
			continue
		}

		nextID := maxID + 1

		switch dbType {
		case "sqlite":
			if err := db.Exec(
				"UPDATE sqlite_sequence SET seq = ? WHERE name = ?",
				maxID, info.TableName,
			).Error; err != nil {
				db.Exec("INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)",
					info.TableName, maxID)
			}

		case "postgres", "postgresql":
			sequenceName := info.TableName + "_id_seq"
			if err := db.Exec(
				fmt.Sprintf("ALTER SEQUENCE IF EXISTS %s RESTART WITH %d", sequenceName, nextID),
			).Error; err != nil {
				log.Printf("Warning: failed to update sequence for %s: %v", info.TableName, err)
			}
		}

		stats.AutoIncrementUpdates++
	}

	return nil
}

// printRestoreSummary 打印还原摘要
func printRestoreSummary(stats *restoreStats, dryRun bool) {
	fmt.Println()
	fmt.Println("========================================")
	if dryRun {
		fmt.Println("       [DRY RUN MODE]")
	}
	fmt.Println("         Restore Summary")
	fmt.Println("========================================")

	if len(stats.Restored) > 0 {
		fmt.Println("Restored records:")
		for table, count := range stats.Restored {
			fmt.Printf("  %-15s %d\n", table+":", count)
		}
	}

	if len(stats.Errors) > 0 {
		fmt.Println("\nErrors:")
		for table, count := range stats.Errors {
			fmt.Printf("  %-15s %d\n", table+":", count)
		}
	}

	if !dryRun {
		fmt.Printf("\nAuto-increment sequences updated: %d\n", stats.AutoIncrementUpdates)
	}

	fmt.Println("========================================")
}

// contains 检查字符串是否在切片中
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
