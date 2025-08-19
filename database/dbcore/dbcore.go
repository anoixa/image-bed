package dbcore

import (
	"context"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"image-bed/config"
	"image-bed/database/models"
	"log"
	"sync"
	"time"
)

var (
	db   *gorm.DB
	once sync.Once
)

// GetDBInstance Get database connection
func GetDBInstance() *gorm.DB {
	once.Do(func() {
		var err error

		cfg := config.Get()
		dbType := cfg.Server.DatabaseConfig.Type
		host := cfg.Server.DatabaseConfig.Host
		port := cfg.Server.DatabaseConfig.Port

		switch dbType {
		case "sqlite", "sqlite3", "":
			path := cfg.Server.DatabaseConfig.DatabaseFilePath
			if path == "" {
				path = "./data/images.db"
			}

			// WAL
			dsn := fmt.Sprintf("%s?_journal_mode=WAL", path)
			db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
				Logger:                 logger.Default.LogMode(logger.Silent),
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
				Logger:                 logger.Default.LogMode(logger.Silent),
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

	return db
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
