package storage

import (
	"context"
	"fmt"
	"github.com/anoixa/image-bed/utils"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	providers       = make(map[uint]Provider)
	providersMu     sync.RWMutex
	defaultProvider Provider
	defaultID       uint
)

type TransferMode string

const (
	TransferModeAuto         TransferMode = "auto"
	TransferModeAlwaysProxy  TransferMode = "always_proxy"
	TransferModeAlwaysDirect TransferMode = "always_direct"
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
	Type      string // "local" | "s3" | "webdav"
	IsDefault bool
	// Local
	LocalPath string
	// S3
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
	Region          string
	ForcePathStyle  bool
	PublicDomain    string
	IsPrivate       bool
	// WebDAV
	WebDAVURL      string
	WebDAVUsername string
	WebDAVPassword string
	WebDAVRootPath string
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

// FileOpener 支持直接打开 *os.File 的存储
type FileOpener interface {
	OpenFile(ctx context.Context, name string) (*os.File, error)
}

// PathProvider is implemented by storage backends that can expose a
// local OS file path for a stored object. Only LocalStorage implements this.
// Callers must not write to or delete the returned path.
type PathProvider interface {
	GetFilePath(storagePath string) (string, error)
}

// StreamProvider 流式传输到 ResponseWriter 的存储
type StreamProvider interface {
	Provider
	StreamTo(ctx context.Context, storagePath string, w http.ResponseWriter) (int64, error)
}

// DirectURLProvider 直链提供者接口
type DirectURLProvider interface {
	GetDirectURL(storagePath string) string
	SupportsDirectLink() bool
	ShouldProxy(imageIsPublic bool, globalMode TransferMode) bool
}

// InitStorage 初始化存储层
func InitStorage(configs []StorageConfig) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	utils.Errorf("[Storage] ============================================")
	utils.Infof("[Storage] Starting storage initialization...")
	utils.Errorf("[Storage] Total configs to initialize: %d", len(configs))
	utils.Errorf("[Storage] --------------------------------------------")

	var initErrors []error
	successCount := 0
	var defaultCfg *StorageConfig

	for i := range configs {
		cfg := &configs[i]

		// 记录默认配置信息
		if cfg.IsDefault {
			defaultCfg = cfg
			utils.Errorf("[Storage] [DEFAULT] ID=%d, Name=%s, Type=%s", cfg.ID, cfg.Name, cfg.Type)
		}

		utils.Errorf("[Storage] Initializing: ID=%d, Name=%s, Type=%s, IsDefault=%v",
			cfg.ID, cfg.Name, cfg.Type, cfg.IsDefault)

		provider, err := createProvider(*cfg)
		if err != nil {
			utils.Errorf("[Storage] [FAILED] ID=%d, Name=%s, Error: %v", cfg.ID, cfg.Name, err)
			initErrors = append(initErrors, fmt.Errorf("ID=%d, Name=%s: %w", cfg.ID, cfg.Name, err))
			continue
		}

		providers[cfg.ID] = provider
		successCount++
		utils.Infof("[Storage] [SUCCESS] ID=%d, Name=%s, Type=%s", cfg.ID, cfg.Name, cfg.Type)

		if cfg.IsDefault {
			defaultProvider = provider
			defaultID = cfg.ID
			utils.Errorf("[Storage] [SET DEFAULT] ID=%d (%s)", cfg.ID, cfg.Name)
		}
	}

	utils.Errorf("[Storage] --------------------------------------------")
	utils.Errorf("[Storage] Initialization Summary:")
	utils.Infof("[Storage]   Total: %d, Success: %d, Failed: %d", len(configs), successCount, len(initErrors))

	if defaultProvider == nil {
		if defaultCfg != nil {
			utils.Errorf("[Storage] [ERROR] Default config (ID=%d, Name=%s) failed to initialize", defaultCfg.ID, defaultCfg.Name)
		}
		utils.Errorf("[Storage] ============================================")
		return fmt.Errorf("no default storage available (checked %d configs, %d failed)", len(configs), len(initErrors))
	}

	utils.Errorf("[Storage] [DEFAULT STORAGE] ID=%d, Name=%s", defaultID, defaultProvider.Name())
	utils.Errorf("[Storage] ============================================")

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

// ProviderInfo 存储提供者信息
type ProviderInfo struct {
	ID        uint
	Name      string
	Type      string
	IsDefault bool
}

// ListProviders 列出所有存储提供者信息
func ListProviders() []ProviderInfo {
	providersMu.RLock()
	defer providersMu.RUnlock()

	result := make([]ProviderInfo, 0, len(providers))
	for id, provider := range providers {
		result = append(result, ProviderInfo{
			ID:        id,
			Name:      provider.Name(),
			Type:      "unknown",
			IsDefault: id == defaultID,
		})
	}
	return result
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
	case "s3":
		return NewS3Storage(S3Config{
			Type:            cfg.Type,
			Endpoint:        cfg.Endpoint,
			Region:          cfg.Region,
			BucketName:      cfg.BucketName,
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey,
			ForcePathStyle:  cfg.ForcePathStyle,
			PublicDomain:    cfg.PublicDomain,
			IsPrivate:       cfg.IsPrivate,
		})
	case "webdav":
		return NewWebDAVStorage(WebDAVConfig{
			URL:      cfg.WebDAVURL,
			Username: cfg.WebDAVUsername,
			Password: cfg.WebDAVPassword,
			RootPath: cfg.WebDAVRootPath,
			Timeout:  30 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
