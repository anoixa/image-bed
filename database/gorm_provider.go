package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/anoixa/image-bed/config"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GormProvider GORM 数据库提供者实现
type GormProvider struct {
	db     *gorm.DB
	dbType string
}

// NewGormProvider 创建新的 GORM 数据库提供者
func NewGormProvider(cfg *config.Config) (*GormProvider, error) {
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

	var db *gorm.DB
	var err error

	switch dbType {
	case "sqlite", "sqlite3", "":
		path := cfg.Server.DatabaseConfig.DatabaseFilePath
		if path == "" {
			path = "./data/images.db"
		}

		// WAL 模式
		dsn := fmt.Sprintf("%s?_journal_mode=WAL", path)
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger:                 gormLogger,
			PrepareStmt:            true,
			SkipDefaultTransaction: true,
		})

		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite3 database: %w", err)
		}
		log.Printf("Using SQLite database file: %s", path)

	case "postgres", "postgresql":
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
			return nil, fmt.Errorf("failed to connect to PostgreSQL database: %w", err)
		}
		log.Printf("Connected to PostgreSQL database on %s:%d", host, port)

	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying DB instance: %w", err)
	}

	// 使用配置文件中的连接池参数
	maxOpenConns := cfg.Server.DatabaseConfig.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 100
	}
	maxIdleConns := cfg.Server.DatabaseConfig.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 10
	}
	connMaxLifetime := cfg.Server.DatabaseConfig.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = 3600
	}

	sqlDB.SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetMaxOpenConns(maxOpenConns)

	return &GormProvider{
		db:     db,
		dbType: dbType,
	}, nil
}

// DB 返回底层 *gorm.DB 实例
func (p *GormProvider) DB() *gorm.DB {
	return p.db
}

// WithContext 返回带上下文的 *gorm.DB
func (p *GormProvider) WithContext(ctx context.Context) *gorm.DB {
	return p.db.WithContext(ctx)
}

// Transaction 在事务中执行函数
func (p *GormProvider) Transaction(fn TxFunc) error {
	return p.db.Transaction(fn)
}

// TransactionWithContext 带上下文的事务执行
func (p *GormProvider) TransactionWithContext(ctx context.Context, fn TxFunc) error {
	return p.db.WithContext(ctx).Transaction(fn)
}

// BeginTransaction 手动控制事务
func (p *GormProvider) BeginTransaction() *gorm.DB {
	return p.db.Begin()
}

// WithTransaction 获取支持手动事务的会话
func (p *GormProvider) WithTransaction() *gorm.DB {
	return p.db.Session(&gorm.Session{SkipDefaultTransaction: true})
}

// AutoMigrate 自动迁移数据库结构
func (p *GormProvider) AutoMigrate(models ...interface{}) error {
	return p.db.AutoMigrate(models...)
}

// SQLDB 返回底层 sql.DB
func (p *GormProvider) SQLDB() (*sql.DB, error) {
	return p.db.DB()
}

// Ping 检查数据库连接
func (p *GormProvider) Ping() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Close 关闭数据库连接
func (p *GormProvider) Close() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	log.Println("Closing database connection...")
	return sqlDB.Close()
}

// Name 返回数据库名称
func (p *GormProvider) Name() string {
	return p.dbType
}
