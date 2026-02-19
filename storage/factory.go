package storage

import (
	"fmt"

	"github.com/anoixa/image-bed/storage/local"
	"github.com/anoixa/image-bed/storage/minio"
)

// providers 存储所有配置的存储提供者
var providers = make(map[uint]Provider)
var defaultProvider Provider
var defaultID uint

// InitStorage 初始化存储层
// 在应用启动时调用，配置从数据库或其他配置源读取
func InitStorage(configs []StorageConfig) error {
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

	// 如果没有配置，使用默认本地存储
	if defaultProvider == nil {
		provider, err := local.NewStorage("./data/upload")
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
	return defaultProvider
}

// GetDefaultID 获取默认存储配置ID
func GetDefaultID() uint {
	return defaultID
}

// GetByID 按ID获取存储提供者
func GetByID(id uint) (Provider, error) {
	provider, ok := providers[id]
	if !ok {
		return nil, fmt.Errorf("storage provider with ID %d not found", id)
	}
	return provider, nil
}

// StorageConfig 存储配置
// 用于从配置源（数据库/文件）读取配置后传递给 InitStorage
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

func createProvider(cfg StorageConfig) (Provider, error) {
	switch cfg.Type {
	case "local":
		return local.NewStorage(cfg.LocalPath)
	case "minio":
		return minio.NewStorage(minio.Config{
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

// CloseDefault 关闭默认存储提供者（如果需要）
func CloseDefault() {
	// 目前存储提供者不需要显式关闭
	// 如果有需要，可以在这里添加关闭逻辑
}
