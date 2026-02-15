package database

import (
	"context"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
)

// Factory 数据库工厂 - 负责创建和管理数据库提供者
type Factory struct {
	provider Provider
}

// NewFactory 创建新的数据库工厂
func NewFactory(cfg *config.Config) (*Factory, error) {
	log.Println("Initializing database provider...")

	provider, err := NewGormProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database provider: %w", err)
	}

	log.Printf("Database provider '%s' initialized successfully", provider.Name())

	return &Factory{
		provider: provider,
	}, nil
}

// GetProvider 获取数据库提供者
func (f *Factory) GetProvider() Provider {
	return f.provider
}

// GetDB 获取底层 GORM DB 实例
func (f *Factory) GetDB() interface{} {
	if f.provider == nil {
		return nil
	}
	return f.provider.DB()
}

// Close 关闭数据库连接
func (f *Factory) Close() error {
	if f.provider != nil {
		return f.provider.Close()
	}
	return nil
}

// AutoMigrate 自动迁移数据库结构
func (f *Factory) AutoMigrate() error {
	if f.provider == nil {
		return fmt.Errorf("database provider not initialized")
	}

	modelsToMigrate := []interface{}{
		&models.User{},
		&models.Device{},
		&models.Image{},
		&models.ApiToken{},
		&models.Album{},
		&models.SystemConfig{},
	}

	log.Println("Running database auto migration...")
	if err := f.provider.AutoMigrate(modelsToMigrate...); err != nil {
		return fmt.Errorf("failed to auto migrate database: %w", err)
	}
	log.Println("Database auto migration completed.")
	return nil
}

// DB 返回底层 *gorm.DB 实例
func (f *Factory) DB() interface{} {
	return f.GetDB()
}

// WithContext 返回带上下文的 DB
func (f *Factory) WithContext(ctx context.Context) interface{} {
	if f.provider == nil {
		return nil
	}
	return f.provider.WithContext(ctx)
}

// Transaction 在事务中执行函数
func (f *Factory) Transaction(fn TxFunc) error {
	if f.provider == nil {
		return fmt.Errorf("database provider not initialized")
	}
	return f.provider.Transaction(fn)
}

// TransactionWithContext 带上下文的事务执行
func (f *Factory) TransactionWithContext(ctx context.Context, fn TxFunc) error {
	if f.provider == nil {
		return fmt.Errorf("database provider not initialized")
	}
	return f.provider.TransactionWithContext(ctx, fn)
}

// Ping 检查数据库连接
func (f *Factory) Ping() error {
	if f.provider == nil {
		return fmt.Errorf("database provider not initialized")
	}
	return f.provider.Ping()
}
