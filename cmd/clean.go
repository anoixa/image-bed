package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/di"
	"github.com/spf13/cobra"
)

// cleanCmd 清理数据库孤儿记录和临时文件
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean orphan database records and temp files",
	Long: `Clean orphan database records and temp files.
This includes:
  - Delete database records without corresponding files
  - Delete storage files without corresponding database records
  - Clean temp folder files`,
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		tempOnly, _ := cmd.Flags().GetBool("temp-only")
		dbOnly, _ := cmd.Flags().GetBool("db-only")
		storageOnly, _ := cmd.Flags().GetBool("storage-only")

		if err := runClean(dryRun, tempOnly, dbOnly, storageOnly); err != nil {
			log.Fatalf("Clean failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().Bool("dry-run", false, "Only show what would be cleaned, don't actually delete")
	cleanCmd.Flags().Bool("temp-only", false, "Only clean temp files")
	cleanCmd.Flags().Bool("db-only", false, "Only clean orphan database records")
	cleanCmd.Flags().Bool("storage-only", false, "Only clean orphan storage files")
}

// cleanStats 清理统计信息
type cleanStats struct {
	orphanDBRecords     int // 数据库孤儿记录数
	orphanStorageFiles  int // 存储孤儿文件数
	deletedTempFiles    int // 删除的临时文件数
	deletedDBRecords    int // 删除的数据库记录数
	deletedStorageFiles int // 删除的存储文件数
	errors              []string
}

// runClean 执行清理
func runClean(dryRun, tempOnly, dbOnly, storageOnly bool) error {
	config.InitConfig()
	cfg := config.Get()

	container := di.NewContainer(cfg)
	if err := container.Init(); err != nil {
		return fmt.Errorf("failed to initialize container: %w", err)
	}
	defer container.Close()

	stats := &cleanStats{}

	// 数据库清理
	if !tempOnly && !storageOnly {
		if err := cleanOrphanDBRecords(container, stats, dryRun); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("clean orphan DB records failed: %v", err))
		}
	}

	// 存储清理
	if !tempOnly && !dbOnly {
		if err := cleanOrphanStorageFiles(container, stats, dryRun); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("clean orphan storage files failed: %v", err))
		}
	}

	// 文件清理
	if !dbOnly && !storageOnly {
		if err := cleanTempFiles(cfg, stats, dryRun); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("clean temp files failed: %v", err))
		}
	}

	printCleanStats(stats, dryRun)

	if len(stats.errors) > 0 {
		return fmt.Errorf("encountered %d errors during cleanup", len(stats.errors))
	}

	return nil
}

// cleanOrphanDBRecords 清理数据库中不存在对应文件的记录
func cleanOrphanDBRecords(container *di.Container, stats *cleanStats, dryRun bool) error {
	log.Println("Checking for orphan database records...")

	db := container.GetDatabaseProvider().DB()
	storageFactory := container.GetStorageFactory()

	var images []models.Image
	if err := db.Find(&images).Error; err != nil {
		return fmt.Errorf("failed to fetch images: %w", err)
	}

	var orphanIDs []uint
	for _, img := range images {
		provider, err := storageFactory.Get(img.StorageDriver)
		if err != nil {
			log.Printf("Warning: unknown storage driver '%s' for image %s", img.StorageDriver, img.Identifier)
			continue
		}

		exists, err := provider.Exists(context.Background(), img.Identifier)
		if err != nil {
			log.Printf("Warning: failed to check existence of %s: %v", img.Identifier, err)
			continue
		}

		if !exists {
			stats.orphanDBRecords++
			orphanIDs = append(orphanIDs, img.ID)
			if dryRun {
				log.Printf("[DRY-RUN] Would delete orphan DB record: ID=%d, Identifier=%s", img.ID, img.Identifier)
			}
		}
	}

	if !dryRun && len(orphanIDs) > 0 {
		// 删除关联的 album_images 记录
		if err := db.Exec("DELETE FROM album_images WHERE image_id IN ?", orphanIDs).Error; err != nil {
			log.Printf("Warning: failed to delete album_image associations: %v", err)
		}

		result := db.Delete(&models.Image{}, "id IN ?", orphanIDs)
		if result.Error != nil {
			return fmt.Errorf("failed to delete orphan images: %w", result.Error)
		}
		stats.deletedDBRecords = int(result.RowsAffected)
		log.Printf("Deleted %d orphan database records", result.RowsAffected)
	}

	return nil
}

// cleanOrphanStorageFiles 清理存储中没有对应数据库记录的文件
func cleanOrphanStorageFiles(container *di.Container, stats *cleanStats, dryRun bool) error {
	log.Println("Checking for orphan storage files...")

	cfg := container.GetConfig()

	if cfg.Server.StorageConfig.Type != "local" {
		log.Printf("Storage type '%s' does not support orphan file detection yet", cfg.Server.StorageConfig.Type)
		return nil
	}

	db := container.GetDatabaseProvider().DB()

	var identifiers []string
	if err := db.Model(&models.Image{}).Pluck("identifier", &identifiers).Error; err != nil {
		return fmt.Errorf("failed to fetch image identifiers: %w", err)
	}

	identifierMap := make(map[string]bool)
	for _, id := range identifiers {
		identifierMap[id] = true
	}

	basePath := cfg.Server.StorageConfig.Local.Path
	if basePath == "" {
		basePath = "./data/upload"
	}

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			return err
		}

		relPath = filepath.ToSlash(relPath)

		if !identifierMap[relPath] {
			stats.orphanStorageFiles++
			if dryRun {
				log.Printf("[DRY-RUN] Would delete orphan file: %s", path)
			} else {
				if err := os.Remove(path); err != nil {
					log.Printf("Warning: failed to delete orphan file %s: %v", path, err)
				} else {
					stats.deletedStorageFiles++
					log.Printf("Deleted orphan file: %s", path)
				}
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk storage directory: %w", err)
	}

	return nil
}

// cleanTempFiles 清理临时文件
func cleanTempFiles(cfg *config.Config, stats *cleanStats, dryRun bool) error {
	log.Println("Checking for temp files...")

	tempDir := "./data/temp"

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("Temp directory does not exist, skipping...")
			return nil
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if dryRun {
			log.Printf("[DRY-RUN] Would check temp file: %s", entry.Name())
		} else {
			path := filepath.Join(tempDir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("Warning: failed to delete temp file %s: %v", entry.Name(), err)
			} else {
				stats.deletedTempFiles++
				log.Printf("Deleted temp file: %s", entry.Name())
			}
		}
	}

	return nil
}

// printCleanStats 打印清理统计
func printCleanStats(stats *cleanStats, dryRun bool) {
	fmt.Println()
	fmt.Println("========================================")
	if dryRun {
		fmt.Println("           [DRY RUN MODE]")
	}
	fmt.Println("         Clean Statistics")
	fmt.Println("========================================")
	fmt.Printf("Orphan DB records found:    %d\n", stats.orphanDBRecords)
	fmt.Printf("Orphan storage files found: %d\n", stats.orphanStorageFiles)
	fmt.Printf("DB records deleted:         %d\n", stats.deletedDBRecords)
	fmt.Printf("Storage files deleted:      %d\n", stats.deletedStorageFiles)
	fmt.Printf("Temp files deleted:         %d\n", stats.deletedTempFiles)
	fmt.Println("========================================")

	if len(stats.errors) > 0 {
		fmt.Println("\nErrors encountered:")
		for _, err := range stats.errors {
			fmt.Printf("  - %s\n", err)
		}
	}
}
