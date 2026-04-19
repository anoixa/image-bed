package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var cleanLog = utils.ForModule("Clean")

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
		initCommandLogger()

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		tempOnly, _ := cmd.Flags().GetBool("temp-only")
		dbOnly, _ := cmd.Flags().GetBool("db-only")
		storageOnly, _ := cmd.Flags().GetBool("storage-only")

		if err := runClean(dryRun, tempOnly, dbOnly, storageOnly); err != nil {
			exitWithErrorf("Clean failed: %v", err)
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
	orphanDBRecords     int
	orphanStorageFiles  int
	deletedTempFiles    int
	deletedDBRecords    int
	deletedStorageFiles int
	errors              []string
}

// runClean 执行清理
func runClean(dryRun, tempOnly, dbOnly, storageOnly bool) error {
	cfg := config.Get()

	db, err := initDB()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	stats := &cleanStats{}

	if !tempOnly && !storageOnly {
		if err := cleanOrphanDBRecords(db, stats, dryRun); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("clean orphan DB records failed: %v", err))
		}
	}

	if !tempOnly && !dbOnly {
		if err := cleanOrphanStorageFiles(db, stats, dryRun); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("clean orphan storage files failed: %v", err))
		}
	}

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
func cleanOrphanDBRecords(db *gorm.DB, stats *cleanStats, dryRun bool) error {
	cleanLog.Infof("Checking for orphan database records")

	var images []models.Image
	if err := db.Find(&images).Error; err != nil {
		return fmt.Errorf("failed to fetch images: %w", err)
	}

	total := len(images)
	cleanLog.Infof("Checking %d images against storage (this may be slow for remote storage backends)", total)

	ctx := context.Background()
	var orphanIDs []uint
	for i, img := range images {
		if i > 0 && i%100 == 0 {
			cleanLog.Infof("Progress: %d/%d checked, %d orphans found so far", i, total, len(orphanIDs))
		}

		exists, err := func() (bool, error) {
			if img.StorageConfigID > 0 {
				provider, provErr := storage.GetByID(img.StorageConfigID)
				if provErr == nil {
					return provider.Exists(ctx, img.StoragePath)
				}
				cleanLog.Warnf("Storage provider for config ID=%d not found, falling back to default: %v", img.StorageConfigID, provErr)
			}
			return storage.GetDefault().Exists(ctx, img.StoragePath)
		}()
		if err != nil {
			cleanLog.Warnf("Failed to check existence of %s: %v", utils.SanitizeLogMessage(img.Identifier), err)
			continue
		}

		if !exists {
			stats.orphanDBRecords++
			orphanIDs = append(orphanIDs, img.ID)
			if dryRun {
				cleanLog.Infof("[DRY-RUN] Would delete orphan DB record: ID=%d, Identifier=%s", img.ID, utils.SanitizeLogMessage(img.Identifier))
			}
		}
	}

	if !dryRun && len(orphanIDs) > 0 {
		// 删除关联的 album_images 记录
		if err := db.Exec("DELETE FROM album_images WHERE image_id IN ?", orphanIDs).Error; err != nil {
			cleanLog.Warnf("Failed to delete album_image associations: %v", err)
		}

		result := db.Delete(&models.Image{}, "id IN ?", orphanIDs)
		if result.Error != nil {
			return fmt.Errorf("failed to delete orphan images: %w", result.Error)
		}
		stats.deletedDBRecords = int(result.RowsAffected)
		cleanLog.Infof("Deleted %d orphan database records", result.RowsAffected)
	}

	return nil
}

// cleanOrphanStorageFiles 清理存储中没有对应数据库记录的文件
func cleanOrphanStorageFiles(db *gorm.DB, stats *cleanStats, dryRun bool) error {
	cleanLog.Infof("Checking for orphan storage files")

	provider := storage.GetDefault()
	if provider == nil {
		return fmt.Errorf("no default storage provider")
	}

	localStorage, ok := provider.(*storage.LocalStorage)
	if !ok {
		cleanLog.Warnf("Storage type '%s' does not support orphan file detection yet", provider.Name())
		return nil
	}

	var storagePaths []string
	if err := db.Model(&models.Image{}).Where("deleted_at IS NULL").Pluck("storage_path", &storagePaths).Error; err != nil {
		return fmt.Errorf("failed to fetch image storage paths: %w", err)
	}

	var variantPaths []string
	if err := db.Table("image_variants").Pluck("storage_path", &variantPaths).Error; err != nil {
		cleanLog.Warnf("Could not fetch variant paths: %v", err)
	}
	storagePaths = append(storagePaths, variantPaths...)

	identifierMap := make(map[string]bool)
	for _, p := range storagePaths {
		identifierMap[filepath.ToSlash(p)] = true
	}

	basePath := localStorage.BasePath()

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
				cleanLog.Infof("[DRY-RUN] Would delete orphan file: %s", path)
			} else {
				if err := os.Remove(path); err != nil {
					cleanLog.Warnf("Failed to delete orphan file %s: %v", path, err)
				} else {
					stats.deletedStorageFiles++
					cleanLog.Infof("Deleted orphan file: %s", path)
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
func cleanTempFiles(_ *config.Config, stats *cleanStats, dryRun bool) error {
	cleanLog.Infof("Checking for temp files")

	tempDir := config.TempDir

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			cleanLog.Infof("Temp directory does not exist, skipping")
			return nil
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if dryRun {
			cleanLog.Infof("[DRY-RUN] Would check temp file: %s", entry.Name())
		} else {
			path := filepath.Join(tempDir, entry.Name())
			if err := os.Remove(path); err != nil {
				cleanLog.Warnf("Failed to delete temp file %s: %v", entry.Name(), err)
			} else {
				stats.deletedTempFiles++
				cleanLog.Infof("Deleted temp file: %s", entry.Name())
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
