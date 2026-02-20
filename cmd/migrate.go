package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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
			log.Fatalf("Migration failed: %v", err)
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
	users       int
	devices     int
	images      int
	albums      int
	tokens      int
	skipped     int // 跳过的记录数
	overwritten int // 覆盖的记录数
	errors      []string
}

// runMigration 执行数据库迁移
func runMigration(fromType, toType, fromDSN, toDSN, fromSQLite, toPostgres string, skipConfirm bool, batchSize int, onConflict string) error {
	// 验证冲突处理策略
	if onConflict != "skip" && onConflict != "overwrite" && onConflict != "error" {
		return fmt.Errorf("invalid on-conflict strategy: %s (must be skip, overwrite, or error)", onConflict)
	}

	// 处理快捷方式参数
	if fromSQLite != "" {
		fromType = "sqlite"
		fromDSN = fromSQLite
	}
	if toPostgres != "" {
		toType = "postgres"
		toDSN = toPostgres
	}

	// 验证参数
	if fromType == "" || toType == "" {
		return fmt.Errorf("both --from-type and --to-type are required")
	}
	if fromDSN == "" || toDSN == "" {
		return fmt.Errorf("both --from-dsn and --to-dsn (or shortcuts) are required")
	}

	// 检查源和目标是否相同
	if fromType == toType && fromDSN == toDSN {
		return fmt.Errorf("source and target databases are the same")
	}

	log.Printf("Migrating from %s to %s", fromType, toType)
	log.Printf("Source: %s", maskDSN(fromDSN))
	log.Printf("Target: %s", maskDSN(toDSN))
	log.Printf("Conflict strategy: %s", onConflict)

	// 连接源数据库
	sourceDB, err := openDatabase(fromType, fromDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to source database: %w", err)
	}
	sqlDB, _ := sourceDB.DB()
	defer sqlDB.Close()

	// 连接目标数据库
	targetDB, err := openDatabase(toType, toDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}
	sqlDB2, _ := targetDB.DB()
	defer sqlDB2.Close()

	// 确认迁移
	if !skipConfirm {
		fmt.Println("\nWarning: This will migrate all data from source to target database.")
		fmt.Printf("Conflict resolution strategy: %s\n", onConflict)
		fmt.Println("Existing data in target database may be affected.")
		fmt.Print("Do you want to continue? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	stats := &migrateStats{}

	// 自动迁移目标数据库结构
	log.Println("Migrating database schema...")
	if err := autoMigrate(targetDB); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	// 迁移数据
	ctx := context.Background()

	log.Println("Migrating users...")
	if err := migrateUsers(ctx, sourceDB, targetDB, stats, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("users migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	log.Println("Migrating devices...")
	if err := migrateDevices(ctx, sourceDB, targetDB, stats, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("devices migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	log.Println("Migrating images...")
	if err := migrateImages(ctx, sourceDB, targetDB, stats, batchSize, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("images migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	log.Println("Migrating albums...")
	if err := migrateAlbums(ctx, sourceDB, targetDB, stats, batchSize, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("albums migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	log.Println("Migrating album_images relationships...")
	if err := migrateAlbumImages(ctx, sourceDB, targetDB, stats, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("album_images migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	log.Println("Migrating API tokens...")
	if err := migrateTokens(ctx, sourceDB, targetDB, stats, onConflict); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("tokens migration failed: %v", err))
		if onConflict == "error" {
			return err
		}
	}

	// 打印统计
	printMigrateStats(stats)

	if len(stats.errors) > 0 {
		return fmt.Errorf("migration completed with %d errors", len(stats.errors))
	}

	log.Println("Migration completed successfully!")
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

	// 设置连接池
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
	return db.AutoMigrate(
		&models.User{},
		&models.Device{},
		&models.Image{},
		&models.Album{},
		&models.ApiToken{},
	)
}

// handleConflict 处理冲突
// 返回值: shouldCreate (是否创建), shouldOverwrite (是否覆盖), error
func handleConflict(targetDB *gorm.DB, model interface{}, where string, args []interface{}, onConflict string) (bool, bool, error) {
	var existing interface{}
	result := targetDB.Where(where, args...).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		// 不存在，可以创建
		return true, false, nil
	}
	if result.Error != nil {
		return false, false, result.Error
	}

	// 已存在
	switch onConflict {
	case "skip":
		return false, false, nil
	case "overwrite":
		return false, true, nil
	case "error":
		return false, false, fmt.Errorf("record already exists: %v", args)
	default:
		return false, false, nil
	}
}

// migrateUsers 迁移用户数据
func migrateUsers(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, onConflict string) error {
	var users []models.User
	if err := sourceDB.WithContext(ctx).Find(&users).Error; err != nil {
		return err
	}

	for _, user := range users {
		shouldCreate, shouldOverwrite, err := handleConflict(
			targetDB.WithContext(ctx),
			&models.User{},
			"id = ?",
			[]interface{}{user.ID},
			onConflict,
		)
		if err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("conflict check failed for user %d: %v", user.ID, err))
			if onConflict == "error" {
				return err
			}
			continue
		}

		if shouldCreate {
			// 处理软删除时间
			if user.DeletedAt.Valid {
				user.DeletedAt = gorm.DeletedAt{Valid: true, Time: user.DeletedAt.Time}
			}

			if err := targetDB.WithContext(ctx).Create(&user).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to migrate user %d: %v", user.ID, err))
				continue
			}
			stats.users++
		} else if shouldOverwrite {
			// 删除旧记录，插入新记录
			targetDB.WithContext(ctx).Where("id = ?", user.ID).Delete(&models.User{})
			if err := targetDB.WithContext(ctx).Create(&user).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to overwrite user %d: %v", user.ID, err))
				continue
			}
			stats.overwritten++
			stats.users++
		} else {
			stats.skipped++
		}
	}

	log.Printf("Migrated %d users (skipped: %d, overwritten: %d)", stats.users, stats.skipped, stats.overwritten)
	return nil
}

// migrateDevices 迁移设备数据
func migrateDevices(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, onConflict string) error {
	var devices []models.Device
	if err := sourceDB.WithContext(ctx).Find(&devices).Error; err != nil {
		return err
	}

	for _, device := range devices {
		shouldCreate, shouldOverwrite, err := handleConflict(
			targetDB.WithContext(ctx),
			&models.Device{},
			"id = ?",
			[]interface{}{device.ID},
			onConflict,
		)
		if err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("conflict check failed for device %d: %v", device.ID, err))
			if onConflict == "error" {
				return err
			}
			continue
		}

		if shouldCreate {
			if err := targetDB.WithContext(ctx).Create(&device).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to migrate device %d: %v", device.ID, err))
				continue
			}
			stats.devices++
		} else if shouldOverwrite {
			targetDB.WithContext(ctx).Where("id = ?", device.ID).Delete(&models.Device{})
			if err := targetDB.WithContext(ctx).Create(&device).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to overwrite device %d: %v", device.ID, err))
				continue
			}
			stats.devices++
		}
	}

	log.Printf("Migrated %d devices", stats.devices)
	return nil
}

// migrateImages 迁移图片数据
func migrateImages(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, batchSize int, onConflict string) error {
	var totalCount int64
	if err := sourceDB.WithContext(ctx).Model(&models.Image{}).Count(&totalCount).Error; err != nil {
		return err
	}

	var offset int
	for {
		var images []models.Image
		if err := sourceDB.WithContext(ctx).Limit(batchSize).Offset(offset).Find(&images).Error; err != nil {
			return err
		}

		if len(images) == 0 {
			break
		}

		for _, image := range images {
			shouldCreate, shouldOverwrite, err := handleConflict(
				targetDB.WithContext(ctx),
				&models.Image{},
				"id = ? OR identifier = ?",
				[]interface{}{image.ID, image.Identifier},
				onConflict,
			)
			if err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("conflict check failed for image %d: %v", image.ID, err))
				if onConflict == "error" {
					return err
				}
				continue
			}

			// 清除关联，避免外键约束问题
			image.Albums = nil

			if shouldCreate {
				if err := targetDB.WithContext(ctx).Create(&image).Error; err != nil {
					stats.errors = append(stats.errors, fmt.Sprintf("failed to migrate image %d: %v", image.ID, err))
					continue
				}
				stats.images++
			} else if shouldOverwrite {
				targetDB.WithContext(ctx).Where("id = ? OR identifier = ?", image.ID, image.Identifier).Delete(&models.Image{})
				if err := targetDB.WithContext(ctx).Create(&image).Error; err != nil {
					stats.errors = append(stats.errors, fmt.Sprintf("failed to overwrite image %d: %v", image.ID, err))
					continue
				}
				stats.images++
			}
		}

		offset += batchSize
		if offset%1000 == 0 {
			log.Printf("Migrated %d/%d images...", stats.images, totalCount)
		}
	}

	log.Printf("Migrated %d images", stats.images)
	return nil
}

// migrateAlbums 迁移相册数据
func migrateAlbums(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, batchSize int, onConflict string) error {
	var albums []models.Album
	if err := sourceDB.WithContext(ctx).Find(&albums).Error; err != nil {
		return err
	}

	for _, album := range albums {
		shouldCreate, shouldOverwrite, err := handleConflict(
			targetDB.WithContext(ctx),
			&models.Album{},
			"id = ?",
			[]interface{}{album.ID},
			onConflict,
		)
		if err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("conflict check failed for album %d: %v", album.ID, err))
			if onConflict == "error" {
				return err
			}
			continue
		}

		// 清除关联
		album.Images = nil

		if shouldCreate {
			if err := targetDB.WithContext(ctx).Create(&album).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to migrate album %d: %v", album.ID, err))
				continue
			}
			stats.albums++
		} else if shouldOverwrite {
			targetDB.WithContext(ctx).Where("id = ?", album.ID).Delete(&models.Album{})
			if err := targetDB.WithContext(ctx).Create(&album).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to overwrite album %d: %v", album.ID, err))
				continue
			}
			stats.albums++
		}
	}

	log.Printf("Migrated %d albums", stats.albums)
	return nil
}

// migrateAlbumImages 迁移相册-图片关联关系
func migrateAlbumImages(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, onConflict string) error {
	// 获取源数据库中的所有关联关系
	type AlbumImage struct {
		AlbumID uint
		ImageID uint
	}

	var relations []AlbumImage
	if err := sourceDB.WithContext(ctx).Raw("SELECT album_id, image_id FROM album_images").Scan(&relations).Error; err != nil {
		// 表可能不存在
		return nil
	}

	for _, rel := range relations {
		// 检查目标数据库是否已存在此关联
		var count int64
		targetDB.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM album_images WHERE album_id = ? AND image_id = ?",
			rel.AlbumID, rel.ImageID,
		).Scan(&count)

		if count > 0 {
			if onConflict == "error" {
				return fmt.Errorf("album_image relation already exists: album_id=%d, image_id=%d", rel.AlbumID, rel.ImageID)
			}
			// skip 和 overwrite 都跳过已存在的关联
			continue
		}

		// 检查关联的相册和图片是否存在
		var albumCount, imageCount int64
		targetDB.WithContext(ctx).Model(&models.Album{}).Where("id = ?", rel.AlbumID).Count(&albumCount)
		targetDB.WithContext(ctx).Model(&models.Image{}).Where("id = ?", rel.ImageID).Count(&imageCount)

		if albumCount == 0 || imageCount == 0 {
			log.Printf("Skipping album_image relation: album_id=%d, image_id=%d (referenced record not found)",
				rel.AlbumID, rel.ImageID)
			continue
		}

		if err := targetDB.WithContext(ctx).Exec(
			"INSERT INTO album_images (album_id, image_id) VALUES (?, ?)",
			rel.AlbumID, rel.ImageID,
		).Error; err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf(
				"failed to migrate album_image relation (album=%d, image=%d): %v",
				rel.AlbumID, rel.ImageID, err))
			continue
		}
	}

	log.Printf("Migrated %d album_image relations", len(relations))
	return nil
}

// migrateTokens 迁移API Token数据
func migrateTokens(ctx context.Context, sourceDB, targetDB *gorm.DB, stats *migrateStats, onConflict string) error {
	var tokens []models.ApiToken
	if err := sourceDB.WithContext(ctx).Find(&tokens).Error; err != nil {
		return nil // tokens表可能不存在，不报错
	}

	for _, token := range tokens {
		shouldCreate, shouldOverwrite, err := handleConflict(
			targetDB.WithContext(ctx),
			&models.ApiToken{},
			"id = ?",
			[]interface{}{token.ID},
			onConflict,
		)
		if err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("conflict check failed for token %d: %v", token.ID, err))
			if onConflict == "error" {
				return err
			}
			continue
		}

		if shouldCreate {
			if err := targetDB.WithContext(ctx).Create(&token).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to migrate token %d: %v", token.ID, err))
				continue
			}
			stats.tokens++
		} else if shouldOverwrite {
			targetDB.WithContext(ctx).Where("id = ?", token.ID).Delete(&models.ApiToken{})
			if err := targetDB.WithContext(ctx).Create(&token).Error; err != nil {
				stats.errors = append(stats.errors, fmt.Sprintf("failed to overwrite token %d: %v", token.ID, err))
				continue
			}
			stats.tokens++
		}
	}

	log.Printf("Migrated %d API tokens", stats.tokens)
	return nil
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
	fmt.Printf("Users migrated:    %d\n", stats.users)
	fmt.Printf("Devices migrated:  %d\n", stats.devices)
	fmt.Printf("Images migrated:   %d\n", stats.images)
	fmt.Printf("Albums migrated:   %d\n", stats.albums)
	fmt.Printf("Tokens migrated:   %d\n", stats.tokens)
	fmt.Printf("Skipped records:   %d\n", stats.skipped)
	fmt.Printf("Overwritten:       %d\n", stats.overwritten)
	fmt.Println("========================================")

	if len(stats.errors) > 0 {
		fmt.Println("\nErrors encountered:")
		for _, err := range stats.errors {
			fmt.Printf("  - %s\n", err)
		}
	}
}
