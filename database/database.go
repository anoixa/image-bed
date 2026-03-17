package database

import (
	"context"
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

// TxFunc 事务函数类型
type TxFunc func(tx *gorm.DB) error

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

	utils.LogIfDevf("Using SQLite database: %s", path)
	return db, nil
}

// newPostgresDB 创建 PostgreSQL 连接
func newPostgresDB(cfg *config.Config, gormLogger logger.Interface) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUsername, cfg.DBPassword, cfg.DBName)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 gormLogger,
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
	}

	utils.LogIfDevf("Using PostgreSQL database: %s@%s:%d/%s", cfg.DBUsername, cfg.DBHost, cfg.DBPort, cfg.DBName)
	return db, nil
}

// newGormLogger 创建 GORM 日志器
func newGormLogger() logger.Interface {
	logLevel := logger.Silent
	colorful := false

	if config.IsDevelopment() {
		logLevel = logger.Info
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
	if err := db.AutoMigrate(
		&models.User{},
		&models.Device{},
		&models.Image{},
		&models.ApiToken{},
		&models.Album{},
		&models.SystemConfig{},
		&models.ImageVariant{},
	); err != nil {
		return err
	}

	// 执行额外的索引修复
	if err := fixSystemConfigIndexes(db); err != nil {
		return err
	}

	return fixImageIdentifierIndexes(db)
}

// fixSystemConfigIndexes 修复 system_configs 表的索引
// 将旧的唯一索引改为条件唯一索引（只针对未删除的记录）
func fixSystemConfigIndexes(db *gorm.DB) error {
	// 获取数据库类型
	var dbType string
	if db.Name() == "sqlite" {
		dbType = "sqlite"
	} else {
		dbType = "postgres"
	}

	// 删除旧索引（如果存在）
	dropOldIndexSQL := `DROP INDEX IF EXISTS idx_system_configs_key`
	if err := db.Exec(dropOldIndexSQL).Error; err != nil {
		log.Printf("[DB Migration] Warning: failed to drop old index: %v", err)
		// 继续执行，不要返回错误
	} else {
		log.Printf("[DB Migration] Dropped old index idx_system_configs_key")
	}

	// 创建新条件索引
	var createIndexSQL string
	if dbType == "sqlite" {
		createIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_key_unique ON system_configs(key) WHERE deleted_at IS NULL`
	} else {
		// PostgreSQL
		createIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_key_unique ON system_configs(key) WHERE deleted_at IS NULL`
	}

	if err := db.Exec(createIndexSQL).Error; err != nil {
		// 如果索引已存在，会报错，忽略
		if !isIndexExistsError(err) {
			return fmt.Errorf("failed to create new index: %w", err)
		}
	} else {
		log.Printf("[DB Migration] Created new partial index idx_key_unique")
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
	var dbType string
	if db.Name() == "sqlite" {
		dbType = "sqlite"
	} else {
		dbType = "postgres"
	}

	if dbType == "sqlite" {
		utils.LogIfDevf("[DB Migration] SQLite detected, skipping partial index for images.identifier")
		return nil
	}

	dropOldIndexSQL := `DROP INDEX IF EXISTS idx_identifier`
	if err := db.Exec(dropOldIndexSQL).Error; err != nil {
		log.Printf("[DB Migration] Warning: failed to drop old index idx_identifier: %v", err)
	} else {
		log.Printf("[DB Migration] Dropped old index idx_identifier")
	}

	dropOldIndexSQL2 := `DROP INDEX IF EXISTS idx_images_identifier`
	if err := db.Exec(dropOldIndexSQL2).Error; err != nil {
		log.Printf("[DB Migration] Warning: failed to drop old index idx_images_identifier: %v", err)
	} else {
		log.Printf("[DB Migration] Dropped old index idx_images_identifier")
	}

	createIndexSQL := `CREATE UNIQUE INDEX IF NOT EXISTS idx_images_identifier_active ON images(identifier) WHERE deleted_at IS NULL`

	if err := db.Exec(createIndexSQL).Error; err != nil {
		if !isIndexExistsError(err) {
			return fmt.Errorf("failed to create partial index for images.identifier: %w", err)
		}
	} else {
		log.Printf("[DB Migration] Created partial unique index idx_images_identifier_active on images.identifier (WHERE deleted_at IS NULL)")
	}

	return nil
}

// Transaction 执行事务
func Transaction(db *gorm.DB, fn TxFunc) error {
	return db.Transaction(fn)
}

// TransactionWithContext 带上下文的事务
func TransactionWithContext(ctx context.Context, db *gorm.DB, fn TxFunc) error {
	return db.WithContext(ctx).Transaction(fn)
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
