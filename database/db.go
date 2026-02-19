package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewDB 创建数据库连接
func NewDB(cfg *config.Config) (*gorm.DB, error) {
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

	// 配置连接池
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

	if config.CommitHash == "n/a" {
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
	return db.AutoMigrate(
		&models.User{},
		&models.Device{},
		&models.Image{},
		&models.ApiToken{},
		&models.Album{},
		&models.SystemConfig{},
		&models.ImageVariant{},
	)
}

// TxFunc 事务函数类型
type TxFunc func(tx *gorm.DB) error

// Transaction 执行事务
func Transaction(db *gorm.DB, fn TxFunc) error {
	return db.Transaction(fn)
}

// TransactionWithContext 带上下文的事务
func TransactionWithContext(ctx context.Context, db *gorm.DB, fn TxFunc) error {
	return db.WithContext(ctx).Transaction(fn)
}
