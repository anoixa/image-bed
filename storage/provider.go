package storage

import (
	"context"
	"io"
)

// Provider 存储提供者接口 - 依赖倒置的核心抽象
// 定义了存储层的基本操作，所有存储实现必须遵循此接口
type Provider interface {
	// SaveWithContext 保存文件到存储
	SaveWithContext(ctx context.Context, identifier string, file io.Reader) error

	// GetWithContext 从存储获取文件
	GetWithContext(ctx context.Context, identifier string) (io.ReadSeeker, error)

	// DeleteWithContext 从存储删除文件
	DeleteWithContext(ctx context.Context, identifier string) error

	// Exists 检查文件是否存在
	Exists(ctx context.Context, identifier string) (bool, error)

	// Health 检查存储健康状态
	Health(ctx context.Context) error

	// Name 返回存储名称
	Name() string
}
