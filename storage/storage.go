package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anoixa/image-bed/utils"
)

var (
	providersMu sync.Mutex
	registryPtr atomic.Pointer[registryState]
	storageLog  = utils.ForModule("Storage")
)

type registryState struct {
	providers       map[uint]Provider
	defaultProvider Provider
	defaultID       uint
}

func init() {
	registryPtr.Store(&registryState{
		providers: make(map[uint]Provider),
	})
}

type TransferMode string

const (
	TransferModeAuto         TransferMode = "auto"
	TransferModeAlwaysProxy  TransferMode = "always_proxy"
	TransferModeAlwaysDirect TransferMode = "always_direct"
)

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
	storageLog.Infof("============================================")
	storageLog.Infof("Starting storage initialization")
	storageLog.Infof("Total configs to initialize: %d", len(configs))
	storageLog.Infof("--------------------------------------------")

	nextProviders := make(map[uint]Provider, len(configs))
	var initErrors []error
	successCount := 0
	var nextDefaultProvider Provider
	var nextDefaultID uint
	var defaultCfg *StorageConfig

	for i := range configs {
		cfg := &configs[i]

		// 记录默认配置信息
		if cfg.IsDefault {
			defaultCfg = cfg
			storageLog.Infof("[DEFAULT] ID=%d, Name=%s, Type=%s", cfg.ID, cfg.Name, cfg.Type)
		}

		storageLog.Infof("Initializing: ID=%d, Name=%s, Type=%s, IsDefault=%v",
			cfg.ID, cfg.Name, cfg.Type, cfg.IsDefault)

		provider, err := createProvider(*cfg)
		if err != nil {
			storageLog.Errorf("[FAILED] ID=%d, Name=%s, Error: %v", cfg.ID, cfg.Name, err)
			initErrors = append(initErrors, fmt.Errorf("ID=%d, Name=%s: %w", cfg.ID, cfg.Name, err))
			continue
		}

		nextProviders[cfg.ID] = provider
		successCount++
		storageLog.Infof("[SUCCESS] ID=%d, Name=%s, Type=%s", cfg.ID, cfg.Name, cfg.Type)

		if cfg.IsDefault {
			nextDefaultProvider = provider
			nextDefaultID = cfg.ID
			storageLog.Infof("[SET DEFAULT] ID=%d (%s)", cfg.ID, cfg.Name)
		}
	}

	storageLog.Infof("--------------------------------------------")
	storageLog.Infof("Initialization Summary")
	storageLog.Infof("Total: %d, Success: %d, Failed: %d", len(configs), successCount, len(initErrors))

	if nextDefaultProvider == nil {
		if defaultCfg != nil {
			storageLog.Errorf("[ERROR] Default config (ID=%d, Name=%s) failed to initialize", defaultCfg.ID, defaultCfg.Name)
		}
		storageLog.Infof("============================================")
		return fmt.Errorf("no default storage available (checked %d configs, %d failed)", len(configs), len(initErrors))
	}

	providersMu.Lock()
	registryPtr.Store(&registryState{
		providers:       nextProviders,
		defaultProvider: nextDefaultProvider,
		defaultID:       nextDefaultID,
	})
	providersMu.Unlock()

	storageLog.Infof("[DEFAULT STORAGE] ID=%d, Name=%s", nextDefaultID, nextDefaultProvider.Name())
	storageLog.Infof("============================================")

	return nil
}

// GetDefault 获取默认存储提供者
func GetDefault() Provider {
	return currentRegistry().defaultProvider
}

// GetDefaultID 获取默认存储配置ID
func GetDefaultID() uint {
	return currentRegistry().defaultID
}

// GetByID 按ID获取存储提供者
func GetByID(id uint) (Provider, error) {
	provider, ok := currentRegistry().providers[id]
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

	next := cloneRegistry(currentRegistry())
	next.providers[cfg.ID] = provider

	if cfg.IsDefault {
		next.defaultProvider = provider
		next.defaultID = cfg.ID
	}

	registryPtr.Store(next)

	return nil
}

// RemoveProvider 动态移除存储提供者
func RemoveProvider(id uint) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	next := cloneRegistry(currentRegistry())
	if _, ok := next.providers[id]; !ok {
		return fmt.Errorf("storage provider with ID %d not found", id)
	}

	if id == next.defaultID {
		return fmt.Errorf("cannot remove default storage provider (ID: %d)", id)
	}

	delete(next.providers, id)
	registryPtr.Store(next)
	return nil
}

// SetDefaultID 动态切换默认存储
func SetDefaultID(id uint) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	next := cloneRegistry(currentRegistry())
	provider, ok := next.providers[id]
	if !ok {
		return fmt.Errorf("storage provider with ID %d not found", id)
	}

	next.defaultProvider = provider
	next.defaultID = id
	registryPtr.Store(next)
	return nil
}

// ListProviderIDs 列出所有可用的存储提供者ID
func ListProviderIDs() []uint {
	state := currentRegistry()
	ids := make([]uint, 0, len(state.providers))
	for id := range state.providers {
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
	state := currentRegistry()
	result := make([]ProviderInfo, 0, len(state.providers))
	for id, provider := range state.providers {
		result = append(result, ProviderInfo{
			ID:        id,
			Name:      provider.Name(),
			Type:      "unknown",
			IsDefault: id == state.defaultID,
		})
	}
	return result
}

// GetProviderCount 获取存储提供者数量
func GetProviderCount() int {
	return len(currentRegistry().providers)
}

func currentRegistry() *registryState {
	state := registryPtr.Load()
	if state != nil {
		return state
	}

	fallback := &registryState{providers: make(map[uint]Provider)}
	if registryPtr.CompareAndSwap(nil, fallback) {
		return fallback
	}
	return registryPtr.Load()
}

func cloneRegistry(state *registryState) *registryState {
	nextProviders := make(map[uint]Provider, len(state.providers))
	for id, provider := range state.providers {
		nextProviders[id] = provider
	}

	return &registryState{
		providers:       nextProviders,
		defaultProvider: state.defaultProvider,
		defaultID:       state.defaultID,
	}
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
