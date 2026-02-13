package storage

import (
	"fmt"
	"log"

	"github.com/anoixa/image-bed/config"
)

// Factory 存储工厂 - 负责创建和管理存储提供者
type Factory struct {
	providers map[string]Provider
	defaultProvider string
}

// NewFactory 创建新的存储工厂
func NewFactory(cfg *config.Config) (*Factory, error) {
	factory := &Factory{
		providers: make(map[string]Provider),
	}

	log.Println("Initializing storage providers...")

	// 初始化本地存储
	if cfg.Server.StorageConfig.Local.Path != "" {
		localProvider, err := NewLocalStorage(cfg.Server.StorageConfig.Local.Path)
		if err != nil {
			log.Printf("Failed to initialize local storage: %v", err)
		} else {
			factory.providers["local"] = localProvider
			log.Println("Successfully initialized 'local' storage provider")
		}
	}

	// 初始化 MinIO 存储
	if cfg.Server.StorageConfig.Minio.Endpoint != "" {
		minioProvider, err := NewMinioStorage(cfg.Server.StorageConfig.Minio)
		if err != nil {
			log.Printf("Failed to initialize minio storage: %v", err)
		} else {
			factory.providers["minio"] = minioProvider
			log.Println("Successfully initialized 'minio' storage provider")
		}
	}

	if len(factory.providers) == 0 {
		return nil, fmt.Errorf("no storage providers were successfully initialized")
	}

	// 设置默认存储
	factory.defaultProvider = cfg.Server.StorageConfig.Type
	if _, ok := factory.providers[factory.defaultProvider]; !ok {
		return nil, fmt.Errorf("default storage type '%s' is not available", factory.defaultProvider)
	}
	log.Printf("Default storage provider set to: '%s'", factory.defaultProvider)

	return factory, nil
}

// Get 获取指定名称的存储提供者
func (f *Factory) Get(name string) (Provider, error) {
	if name == "" {
		name = f.defaultProvider
	}

	provider, ok := f.providers[name]
	if !ok {
		return nil, fmt.Errorf("storage provider '%s' not found", name)
	}
	return provider, nil
}

// GetDefault 获取默认存储提供者
func (f *Factory) GetDefault() Provider {
	provider, _ := f.Get(f.defaultProvider)
	return provider
}

// GetDefaultName 获取默认存储提供者名称
func (f *Factory) GetDefaultName() string {
	return f.defaultProvider
}

// ListProviders 列出所有可用的存储提供者名称
func (f *Factory) ListProviders() []string {
	names := make([]string, 0, len(f.providers))
	for name := range f.providers {
		names = append(names, name)
	}
	return names
}
