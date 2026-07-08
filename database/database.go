package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	databaseLog    = utils.ForModule("Database")
	dbMigrationLog = utils.ForModule("DBMigration")
)

// New 创建数据库连接
func New(cfg *config.Config) (*gorm.DB, error) {
	dbType := cfg.DBType
	if dbType == "" {
		dbType = "sqlite"
	}

	// 配置 GORM 日志
	gormLogger := newGormLogger()

	var db *gorm.DB
	var err error

	switch dbType {
	case "sqlite", "sqlite3":
		db, err = newSQLiteDB(cfg, gormLogger)
	case "postgres", "postgresql":
		db, err = newPostgresDB(cfg, gormLogger)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return nil, err
	}

	configurePool(db, cfg)

	return db, nil
}

// newSQLiteDB 创建 SQLite 连接
func newSQLiteDB(cfg *config.Config, gormLogger logger.Interface) (*gorm.DB, error) {
	path := cfg.DBFilePath
	if path == "" {
		path = "./data/images.db"
	}

	// WAL 模式
	dsn := fmt.Sprintf("%s?_journal_mode=WAL", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                 gormLogger,
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	databaseLog.Debugf("Using SQLite database: %s", path)
	return db, nil
}

// newPostgresDB 创建 PostgreSQL 连接
func newPostgresDB(cfg *config.Config, gormLogger logger.Interface) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUsername, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 gormLogger,
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
	}

	databaseLog.Debugf("Using PostgreSQL database: %s@%s:%d/%s", cfg.DBUsername, cfg.DBHost, cfg.DBPort, cfg.DBName)
	return db, nil
}

// newGormLogger 创建 GORM 日志器
func newGormLogger() logger.Interface {
	logLevel := logger.Silent
	colorful := false

	if config.IsDevelopment() {
		logLevel = logger.Warn
		colorful = true
	}

	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  colorful,
		},
	)
}

// configurePool 配置连接池
func configurePool(db *gorm.DB, cfg *config.Config) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}

	if cfg.DBMaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.DBConnMaxLifetime) * time.Second)
	}
}

// AutoMigrate 自动迁移数据库结构
func AutoMigrate(db *gorm.DB) error {
	// Models are sourced from the durable-table manifest (manifest.go), the
	// single authoritative list shared with backup/restore/migrate.
	if err := db.AutoMigrate(MigrationModels()...); err != nil {
		return err
	}

	// 执行额外的索引修复
	if err := fixSystemConfigIndexes(db); err != nil {
		return err
	}

	return fixImageIdentifierIndexes(db)
}

// fixSystemConfigIndexes 修复 system_configs 表的索引
func fixSystemConfigIndexes(db *gorm.DB) error {
	if dropped, err := dropIndexIfExists(db, &models.SystemConfig{}, "idx_system_configs_key"); err != nil {
		dbMigrationLog.Warnf("Failed to drop old index idx_system_configs_key: %v", err)
	} else if dropped {
		dbMigrationLog.Infof("Dropped old index idx_system_configs_key")
	}

	if db.Migrator().HasIndex(&models.SystemConfig{}, "idx_key_unique") {
		return nil
	}

	createIndexSQL := `CREATE UNIQUE INDEX IF NOT EXISTS idx_key_unique ON system_configs(key) WHERE deleted_at IS NULL`
	if err := db.Exec(createIndexSQL).Error; err != nil {
		if !isIndexExistsError(err) {
			return fmt.Errorf("failed to create new index: %w", err)
		}
	} else {
		dbMigrationLog.Infof("Created new partial index idx_key_unique")
	}

	return nil
}

// isIndexExistsError 检查是否为索引已存在的错误
func isIndexExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// SQLite: "index idx_key_unique already exists"
	// PostgreSQL: "relation \"idx_key_unique\" already exists"
	return strings.Contains(errStr, "already exists")
}

// fixImageIdentifierIndexes 创建部分唯一索引，只在未删除记录上强制 identifier 唯一性
func fixImageIdentifierIndexes(db *gorm.DB) error {
	if dropped, err := dropIndexIfExists(db, &models.Image{}, "idx_identifier"); err != nil {
		dbMigrationLog.Warnf("Failed to drop old index idx_identifier: %v", err)
	} else if dropped {
		dbMigrationLog.Infof("Dropped old index idx_identifier")
	}

	if dropped, err := dropIndexIfExists(db, &models.Image{}, "idx_images_identifier"); err != nil {
		dbMigrationLog.Warnf("Failed to drop old index idx_images_identifier: %v", err)
	} else if dropped {
		dbMigrationLog.Infof("Dropped old index idx_images_identifier")
	}

	if db.Migrator().HasIndex(&models.Image{}, "idx_images_identifier_active") {
		return nil
	}

	createIndexSQL := `CREATE UNIQUE INDEX IF NOT EXISTS idx_images_identifier_active ON images(identifier) WHERE deleted_at IS NULL`

	if err := db.Exec(createIndexSQL).Error; err != nil {
		if hasActiveDuplicateImageIdentifiers(db) {
			dbMigrationLog.Warnf("Skipped unique index idx_images_identifier_active because active duplicate image identifiers already exist; new uploads now generate random identifiers, but existing duplicates should be repaired manually")
			return nil
		}
		if !isIndexExistsError(err) {
			return fmt.Errorf("failed to create partial index for images.identifier: %w", err)
		}
	} else {
		dbMigrationLog.Infof("Created partial unique index idx_images_identifier_active on images.identifier (WHERE deleted_at IS NULL)")
	}

	return nil
}

func dropIndexIfExists(db *gorm.DB, model any, indexName string) (bool, error) {
	if !db.Migrator().HasIndex(model, indexName) {
		return false, nil
	}
	return true, db.Exec("DROP INDEX IF EXISTS " + indexName).Error
}

func hasActiveDuplicateImageIdentifiers(db *gorm.DB) bool {
	var duplicateCount int64
	err := db.Raw(`
		SELECT COUNT(*)
		FROM (
			SELECT identifier
			FROM images
			WHERE deleted_at IS NULL
			GROUP BY identifier
			HAVING COUNT(*) > 1
		) AS duplicate_identifiers
	`).Scan(&duplicateCount).Error
	if err != nil {
		dbMigrationLog.Warnf("Failed to check duplicate image identifiers: %v", err)
		return false
	}
	return duplicateCount > 0
}

// Close 关闭数据库连接
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
