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
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/di"
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

// runBackup 执行备份
func runBackup(outputFile string, tables []string, keepDir bool) error {
	config.InitConfig()
	cfg := config.Get()

	container := di.NewContainer(cfg)
	if err := container.Init(); err != nil {
		return fmt.Errorf("failed to initialize container: %w", err)
	}
	defer container.Close()

	db := container.GetDatabaseProvider().DB()

	// 确定输出文件路径
	if outputFile == "" {
		timestamp := time.Now().Format("20060102_150405")
		outputFile = filepath.Join("./backups", fmt.Sprintf("backup_%s.tar.gz", timestamp))
	}

	// 确保输出目录存在
	outputDir := filepath.Dir(outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// 创建临时目录存放备份文件
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("image-bed-backup-%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 如果不是保留目录模式，备份完成后删除临时目录
	if !keepDir {
		defer os.RemoveAll(tempDir)
	}

	log.Printf("Starting backup to: %s", outputFile)

	// 备份元数据
	metadata := &backupMetadata{
		Version:     "1.0",
		Timestamp:   time.Now(),
		Database:    cfg.Server.DatabaseConfig.Type,
		RecordCount: make(map[string]int64),
	}

	// 确定要备份的表
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

	// 写入元数据文件
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

// backupTable 备份单张表
func backupTable(db *gorm.DB, tableName, outputDir string) (int64, error) {
	outputFile := filepath.Join(outputDir, tableName+".jsonl")
	file, err := os.Create(outputFile)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	var count int64

	switch tableName {
	case "users":
		count, err = backupUsers(db, encoder)
	case "devices":
		count, err = backupDevices(db, encoder)
	case "images":
		count, err = backupImages(db, encoder)
	case "albums":
		count, err = backupAlbums(db, encoder)
	case "album_images":
		count, err = backupAlbumImages(db, encoder)
	case "api_tokens":
		count, err = backupTokens(db, encoder)
	default:
		return 0, fmt.Errorf("unknown table: %s", tableName)
	}

	if err != nil {
		return count, err
	}

	return count, nil
}

// backupUsers 备份用户表
func backupUsers(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	var users []models.User
	if err := db.Find(&users).Error; err != nil {
		return 0, err
	}

	var count int64
	for _, user := range users {
		if err := encoder.Encode(user); err != nil {
			log.Printf("Warning: failed to encode user %d: %v", user.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// backupDevices 备份设备表
func backupDevices(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	var devices []models.Device
	if err := db.Find(&devices).Error; err != nil {
		return 0, err
	}

	var count int64
	for _, device := range devices {
		if err := encoder.Encode(device); err != nil {
			log.Printf("Warning: failed to encode device %d: %v", device.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// backupImages 备份图片表
func backupImages(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	var images []models.Image
	if err := db.Find(&images).Error; err != nil {
		return 0, err
	}

	var count int64
	for _, image := range images {
		// 清除关联避免循环引用
		image.Albums = nil

		if err := encoder.Encode(image); err != nil {
			log.Printf("Warning: failed to encode image %d: %v", image.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// backupAlbums 备份相册表
func backupAlbums(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	var albums []models.Album
	if err := db.Find(&albums).Error; err != nil {
		return 0, err
	}

	var count int64
	for _, album := range albums {
		// 清除关联避免循环引用
		album.Images = nil

		if err := encoder.Encode(album); err != nil {
			log.Printf("Warning: failed to encode album %d: %v", album.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// backupAlbumImages 备份相册-图片关联表
func backupAlbumImages(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	type AlbumImage struct {
		AlbumID uint `json:"album_id"`
		ImageID uint `json:"image_id"`
	}

	var relations []AlbumImage
	if err := db.Raw("SELECT album_id, image_id FROM album_images").Scan(&relations).Error; err != nil {
		// 表可能不存在
		return 0, nil
	}

	var count int64
	for _, rel := range relations {
		if err := encoder.Encode(rel); err != nil {
			log.Printf("Warning: failed to encode album_image relation: %v", err)
			continue
		}
		count++
	}

	return count, nil
}

// backupTokens 备份 API Token 表
func backupTokens(db *gorm.DB, encoder *json.Encoder) (int64, error) {
	var tokens []models.ApiToken
	if err := db.Find(&tokens).Error; err != nil {
		return 0, nil // 表可能不存在
	}

	var count int64
	for _, token := range tokens {
		if err := encoder.Encode(token); err != nil {
			log.Printf("Warning: failed to encode token %d: %v", token.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// writeJSONFile 写入 JSON 文件
func writeJSONFile(path string, data interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// createTarGz 创建 tar.gz 压缩包
func createTarGz(sourceDir, outputFile string) error {
	// 创建输出文件
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// 创建 gzip 写入器
	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	// 创建 tar 写入器
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// 遍历源目录
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 获取相对路径
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// 跳过目录本身
		if relPath == "." {
			return nil
		}

		// 创建 tar 头
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// 如果不是目录，写入文件内容
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return err
			}
		}

		return nil
	})
}

// printBackupSummary 打印备份摘要
func printBackupSummary(metadata *backupMetadata, outputFile string) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("          Backup Summary")
	fmt.Println("========================================")
	fmt.Printf("Archive:   %s\n", outputFile)
	fmt.Printf("Version:   %s\n", metadata.Version)
	fmt.Printf("Database:  %s\n", metadata.Database)
	fmt.Printf("Timestamp: %s\n", metadata.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Println("----------------------------------------")
	fmt.Println("Record Count:")
	for table, count := range metadata.RecordCount {
		fmt.Printf("  %-15s %d\n", table+":", count)
	}
	fmt.Println("========================================")
}
