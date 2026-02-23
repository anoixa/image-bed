package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	providers       = make(map[uint]Provider)
	providersMu     sync.RWMutex
	defaultProvider Provider
	defaultID       uint
)

// ImageStream 图片流结构
type ImageStream struct {
	Reader      io.ReadSeeker
	ContentType string
	Size        int64
}

// StorageConfig 存储配置
type StorageConfig struct {
	ID        uint
	Name      string
	Type      string // "local" or "minio"
	IsDefault bool
	// Local
	LocalPath string
	// MinIO
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
}

// Provider 存储提供者接口
type Provider interface {
	// SaveWithContext 保存文件到存储
	SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error

	// GetWithContext 从存储获取文件
	GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error)

	// DeleteWithContext 从存储删除文件
	DeleteWithContext(ctx context.Context, storagePath string) error

	// Exists 检查文件是否存在
	Exists(ctx context.Context, storagePath string) (bool, error)

	// Health 检查存储健康状态
	Health(ctx context.Context) error

	// Name 返回存储名称
	Name() string
}
	
// FileOpener 支持直接打开 *os.File 的存储（用于零拷贝传输）
type FileOpener interface {
	OpenFile(ctx context.Context, name string) (*os.File, error)
}
	
	// InitStorage 初始化存储层
func InitStorage(configs []StorageConfig) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	for _, cfg := range configs {
		provider, err := createProvider(cfg)
		if err != nil {
			return fmt.Errorf("failed to create storage %s: %w", cfg.Name, err)
		}
		providers[cfg.ID] = provider
		if cfg.IsDefault {
			defaultProvider = provider
			defaultID = cfg.ID
		}
	}

	// 如果没有配置则使用默认本地存储
	if defaultProvider == nil {
		provider, err := NewLocalStorage("./data/upload")
		if err != nil {
			return fmt.Errorf("failed to create default storage: %w", err)
		}
		providers[0] = provider
		defaultProvider = provider
		defaultID = 0
	}

	return nil
}

// GetDefault 获取默认存储提供者
func GetDefault() Provider {
	providersMu.RLock()
	defer providersMu.RUnlock()
	return defaultProvider
}

// GetDefaultID 获取默认存储配置ID
func GetDefaultID() uint {
	providersMu.RLock()
	defer providersMu.RUnlock()
	return defaultID
}

// GetByID 按ID获取存储提供者
func GetByID(id uint) (Provider, error) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	provider, ok := providers[id]
	if !ok {
		return nil, fmt.Errorf("storage provider with ID %d not found", id)
	}
	return provider, nil
}

// AddOrUpdateProvider 动态添加或更新存储提供者
func AddOrUpdateProvider(cfg StorageConfig) error {
	provider, err := createProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	providersMu.Lock()
	defer providersMu.Unlock()

	providers[cfg.ID] = provider

	if cfg.IsDefault {
		defaultProvider = provider
		defaultID = cfg.ID
	}

	return nil
}

// RemoveProvider 动态移除存储提供者
func RemoveProvider(id uint) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	if _, ok := providers[id]; !ok {
		return fmt.Errorf("storage provider with ID %d not found", id)
	}

	// 不能移除默认存储
	if id == defaultID {
		return fmt.Errorf("cannot remove default storage provider (ID: %d)", id)
	}

	delete(providers, id)
	return nil
}

// SetDefaultID 动态切换默认存储
func SetDefaultID(id uint) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	provider, ok := providers[id]
	if !ok {
		return fmt.Errorf("storage provider with ID %d not found", id)
	}

	defaultProvider = provider
	defaultID = id
	return nil
}

// ListProviderIDs 列出所有可用的存储提供者ID
func ListProviderIDs() []uint {
	providersMu.RLock()
	defer providersMu.RUnlock()

	ids := make([]uint, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	return ids
}

// GetProviderCount 获取存储提供者数量
func GetProviderCount() int {
	providersMu.RLock()
	defer providersMu.RUnlock()
	return len(providers)
}

func createProvider(cfg StorageConfig) (Provider, error) {
	switch cfg.Type {
	case "local":
		return NewLocalStorage(cfg.LocalPath)
	case "minio":
		return NewMinioStorage(MinioConfig{
			Endpoint:        cfg.Endpoint,
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey,
			UseSSL:          cfg.UseSSL,
			BucketName:      cfg.BucketName,
		})
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
