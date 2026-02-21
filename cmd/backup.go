package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// backupCmd 数据库备份命令
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup database to JSONL archive",
	Long: `Backup database to JSONL format and pack into tar.gz archive.

Example:
  # Backup to default file (./data/backups/backup_YYYYMMDD_HHMMSS.tar.gz)
  image-bed backup

  # Backup to specific file
  image-bed backup --output ./my-backup.tar.gz

  # Backup specific tables only
  image-bed backup --tables users,images`,
	Run: func(cmd *cobra.Command, args []string) {
		outputFile, _ := cmd.Flags().GetString("output")
		tables, _ := cmd.Flags().GetStringSlice("tables")
		keepDir, _ := cmd.Flags().GetBool("keep-dir")

		if err := runBackup(outputFile, tables, keepDir); err != nil {
			log.Fatalf("Backup failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().StringP("output", "o", "", "Output tar.gz file path (default: ./backups/backup_YYYYMMDD_HHMMSS.tar.gz)")
	backupCmd.Flags().StringSliceP("tables", "t", []string{}, "Specific tables to backup (default: all)")
	backupCmd.Flags().Bool("keep-dir", false, "Keep temporary directory after creating archive")
}

// backupMetadata 备份元数据
type backupMetadata struct {
	Version     string           `json:"version"`
	Timestamp   time.Time        `json:"timestamp"`
	Database    string           `json:"database"`
	Tables      []string         `json:"tables"`
	RecordCount map[string]int64 `json:"record_count"`
}

// albumImageRecord album_images 关联记录
type albumImageRecord struct {
	AlbumID uint `json:"album_id"`
	ImageID uint `json:"image_id"`
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
func runBackup(outputFile string, tables []string, keepDir bool) error {
	config.InitConfig()
	cfg := config.Get()

	db, err := initDB()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	if outputFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		outputFile = filepath.Join("./backups", fmt.Sprintf("backup_%s.tar.gz", timestamp))
	}

	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("image-bed-backup-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	if !keepDir {
		defer os.RemoveAll(tempDir)
	}

	log.Printf("Starting backup to: %s", outputFile)

	metadata := &backupMetadata{
		Version:     "1.0",
		Timestamp:   time.Now(),
		Database:    cfg.DBType,
		RecordCount: make(map[string]int64),
	}

	if len(tables) == 0 {
		tables = []string{"users", "devices", "images", "albums", "album_images", "api_tokens"}
	}
	metadata.Tables = tables

	// 备份每张表
	for _, table := range tables {
		count, err := backupTable(db, table, tempDir)
		if err != nil {
			log.Printf("Warning: failed to backup table %s: %v", table, err)
			continue
		}
		metadata.RecordCount[table] = count
		log.Printf("Backed up %d records from table: %s", count, table)
	}

	metadataPath := filepath.Join(tempDir, "metadata.json")
	if err := writeJSONFile(metadataPath, metadata); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// 打包成 tar.gz
	if err := createTarGz(tempDir, outputFile); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	log.Printf("Backup completed successfully: %s", outputFile)
	printBackupSummary(metadata, outputFile)

	return nil
}

// backupTable 备份单张表到 JSONL 文件
func backupTable(db *gorm.DB, table string, tempDir string) (int64, error) {
	outputPath := filepath.Join(tempDir, table+".jsonl")
	file, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	var count int64

	switch table {
	case "users":
		var records []models.User
		if err := db.Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	case "devices":
		var records []models.Device
		if err := db.Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	case "images":
		var records []models.Image
		if err := db.Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	case "albums":
		var records []models.Album
		if err := db.Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	case "album_images":
		// 相册图片关联表需要特殊处理
		var records []albumImageRecord
		if err := db.Table("album_images").Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	case "api_tokens":
		var records []models.ApiToken
		if err := db.Find(&records).Error; err != nil {
			return 0, err
		}
		for _, r := range records {
			if err := encoder.Encode(r); err != nil {
				return 0, err
			}
			count++
		}
	default:
		return 0, fmt.Errorf("unknown table: %s", table)
	}

	return count, nil
}

// writeJSONFile 写入 JSON 文件
func writeJSONFile(path string, data interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// createTarGz 创建 tar.gz 归档
func createTarGz(sourceDir, targetFile string) error {
	file, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gzWriter := gzip.NewWriter(file)
	defer func() { _ = gzWriter.Close() }()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

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
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			data, err := os.Open(path)
			if err != nil {
				return err
			}
			defer data.Close()

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
