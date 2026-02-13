package database

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// TxFunc 事务函数类型
type TxFunc func(tx *gorm.DB) error

// Provider 数据库提供者接口 - 依赖倒置的核心抽象
// 定义了数据库访问的基本操作，所有数据库实现必须遵循此接口
type Provider interface {
	// DB 返回底层 *gorm.DB 实例
	DB() *gorm.DB

	// WithContext 返回带上下文的 *gorm.DB
	WithContext(ctx context.Context) *gorm.DB

	// Transaction 在事务中执行函数
	Transaction(fn TxFunc) error

	// TransactionWithContext 带上下文的事务执行
	TransactionWithContext(ctx context.Context, fn TxFunc) error

	// BeginTransaction 手动控制事务
	BeginTransaction() *gorm.DB

	// WithTransaction 获取支持手动事务的会话
	WithTransaction() *gorm.DB

	// AutoMigrate 自动迁移数据库结构
	AutoMigrate(models ...interface{}) error

	// SQLDB 返回底层 sql.DB
	SQLDB() (*sql.DB, error)

	// Ping 检查数据库连接
	Ping() error

	// Close 关闭数据库连接
	Close() error

	// Name 返回数据库名称
	Name() string
}
