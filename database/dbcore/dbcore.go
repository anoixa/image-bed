package dbcore

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once
)

// GetDBInstance Get database connection
func GetDBInstance() *gorm.DB {
	if db == nil {
		log.Fatal("Database is not initialized.")
	}
	return db
}

// InitDB init database
func InitDB(cfg *config.Config) {
	once.Do(func() {
		var err error

		dbType := cfg.Server.DatabaseConfig.Type
		host := cfg.Server.DatabaseConfig.Host
		port := cfg.Server.DatabaseConfig.Port

		var gormLogger logger.Interface
		if config.CommitHash != "n/a" {
			gormLogger = logger.New(
				log.New(os.Stdout, "\r\n", log.LstdFlags),
				logger.Config{
					SlowThreshold:             200 * time.Millisecond,
					LogLevel:                  logger.Silent,
					IgnoreRecordNotFoundError: true,
					Colorful:                  false,
				},
			)
		} else {
			gormLogger = logger.New(
				log.New(os.Stdout, "\r\n", log.LstdFlags),
				logger.Config{
					SlowThreshold:             200 * time.Millisecond,
					LogLevel:                  logger.Info,
					IgnoreRecordNotFoundError: true,
					Colorful:                  true,
				},
			)
		}

		switch dbType {
		case "sqlite", "sqlite3", "":
			path := cfg.Server.DatabaseConfig.DatabaseFilePath
			if path == "" {
				path = "./data/images.db"
			}

			// WAL
			dsn := fmt.Sprintf("%s?_journal_mode=WAL", path)
			db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
				Logger:                 gormLogger,
				PrepareStmt:            true,
				SkipDefaultTransaction: true,
			})

			if err != nil {
				log.Fatalf("Failed to connect to SQLite3 database: %v", err)
			}
			log.Printf("Using SQLite database file: %s", path)
		case "postgres", "postgresql":
			//组装dsn
			dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
				cfg.Server.DatabaseConfig.Host,
				cfg.Server.DatabaseConfig.Port,
				cfg.Server.DatabaseConfig.Username,
				cfg.Server.DatabaseConfig.Password,
				cfg.Server.DatabaseConfig.Database,
			)

			db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
				Logger:                 gormLogger,
				PrepareStmt:            true,
				SkipDefaultTransaction: true,
			})

			if err != nil {
				log.Fatalf("Failed to connect to PostgreSQL database: %v", err)
			}
			log.Printf("Connected to PostgreSQL database on %s:%d", host, port)

		default:
			log.Fatalf("Unsupported database type: %s", dbType)
		}

		sqlDB, err := db.DB()
		if err != nil {
			log.Fatal("Failed to get underlying DB instance：", err)
		}
		sqlDB.SetConnMaxLifetime(time.Hour)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)

		//err = AutoMigrateDB(db)
		//if err != nil {
		//	log.Fatalf("Auto migrate failed: %v", err)
		//}
	})
}

func CloseDB() error {
	if db == nil {
		log.Println("Database connection is not initialized, no need to close.")
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	log.Println("Closing database connection...")
	return sqlDB.Close()
}

// AutoMigrateDB auto DDL
func AutoMigrateDB(db *gorm.DB) error {
	modelsToMigrate := []interface{}{
		&models.User{},
		&models.Device{},
		&models.Image{},
		&models.ApiToken{},
	}

	err := db.AutoMigrate(modelsToMigrate...)
	if err != nil {
		return fmt.Errorf("failed to auto migrate database schema: %w", err)
	}
	log.Println("Database auto migration completed.")
	return nil
}

// Transaction 执行事务操作的通用函数
func Transaction(fn func(tx *gorm.DB) error) error {
	return GetDBInstance().Transaction(fn)
}

// TransactionWithContext 带上下文的事务执行
func TransactionWithContext(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return GetDBInstance().WithContext(ctx).Transaction(fn)
}

// BeginTransaction 手动控制事务
func BeginTransaction() *gorm.DB {
	return GetDBInstance().Begin()
}

// WithTransaction 获取支持手动事务的会话
func WithTransaction() *gorm.DB {
	return GetDBInstance().Session(&gorm.Session{SkipDefaultTransaction: true})
}
