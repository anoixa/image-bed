package storage

import (
	"context"
	"errors"
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

// 存储不可用的哨兵错误，供上层用 errors.Is 判断并映射为 503。
var (
	// ErrNoDefaultStorage 表示当前没有可用的默认存储。
	ErrNoDefaultStorage = errors.New("no default storage configured")
	// ErrProviderNotFound 表示指定 ID 的存储 Provider 未加载。
	ErrProviderNotFound = errors.New("storage provider not found")
	// ErrProviderDisabled 表示指定 ID 的存储 Provider 已加载但当前不可写（被禁用）。
	ErrProviderDisabled = errors.New("storage provider disabled")
	// ErrCannotDisableDefaultProvider 表示不能把当前默认存储标记为不可写。
	ErrCannotDisableDefaultProvider = errors.New("cannot disable default storage provider")
)

// providerEntry 包装一个已加载的 Provider 及其可写标志（== DB is_enabled）。
type providerEntry struct {
	provider Provider
	writable bool
}

type registryState struct {
	providers       map[uint]providerEntry
	defaultProvider Provider
	defaultID       uint
}

func init() {
	registryPtr.Store(&registryState{
		providers: make(map[uint]providerEntry),
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
	IsEnabled bool
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

	// GetWithContext 从存储获取文件。返回 io.ReadSeekCloser，调用方用完须 Close 以释放底层资源/临时文件。
	GetWithContext(ctx context.Context, storagePath string) (io.ReadSeekCloser, error)

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

// ObjectInfo describes cheap-to-fetch metadata for a stored object.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// ObjectInfoProvider exposes object metadata without downloading the object.
type ObjectInfoProvider interface {
	GetObjectInfo(ctx context.Context, storagePath string) (ObjectInfo, error)
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

	nextProviders := make(map[uint]providerEntry, len(configs))
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

		storageLog.Infof("Initializing: ID=%d, Name=%s, Type=%s, IsDefault=%v, IsEnabled=%v",
			cfg.ID, cfg.Name, cfg.Type, cfg.IsDefault, cfg.IsEnabled)

		provider, err := createProvider(*cfg)
		if err != nil {
			storageLog.Errorf("[FAILED] ID=%d, Name=%s, Error: %v", cfg.ID, cfg.Name, err)
			initErrors = append(initErrors, fmt.Errorf("ID=%d, Name=%s: %w", cfg.ID, cfg.Name, err))
			continue
		}

		nextProviders[cfg.ID] = providerEntry{provider: provider, writable: cfg.IsEnabled}
		successCount++
		storageLog.Infof("[SUCCESS] ID=%d, Name=%s, Type=%s, writable=%v", cfg.ID, cfg.Name, cfg.Type, cfg.IsEnabled)

		// 只有已启用且声明为默认的配置才能成为运行时默认。
		if cfg.IsDefault && cfg.IsEnabled {
			nextDefaultProvider = provider
			nextDefaultID = cfg.ID
			storageLog.Infof("[SET DEFAULT] ID=%d (%s)", cfg.ID, cfg.Name)
		}
	}

	storageLog.Infof("--------------------------------------------")
	storageLog.Infof("Initialization Summary")
	storageLog.Infof("Total: %d, Success: %d, Failed: %d", len(configs), successCount, len(initErrors))

	// 始终先发布已成功加载的 provider（可读集合），保证历史图片即使在没有可写默认时也可读。
	providersMu.Lock()
	registryPtr.Store(&registryState{
		providers:       nextProviders,
		defaultProvider: nextDefaultProvider,
		defaultID:       nextDefaultID,
	})
	providersMu.Unlock()

	if nextDefaultProvider == nil {
		// 退化：若没有可写的声明默认，回退到第一个 enabled provider。
		if fallbackID, fallbackProvider, ok := firstEnabledProvider(nextProviders); ok {
			providersMu.Lock()
			next := cloneRegistry(currentRegistry())
			next.defaultProvider = fallbackProvider
			next.defaultID = fallbackID
			registryPtr.Store(next)
			providersMu.Unlock()
			storageLog.Warnf("[DEGRADED] declared default unavailable; using first enabled provider ID=%d", fallbackID)
			storageLog.Infof("============================================")
			return nil
		}
		if defaultCfg != nil {
			storageLog.Errorf("[ERROR] Default config (ID=%d, Name=%s) is disabled or failed", defaultCfg.ID, defaultCfg.Name)
		}
		storageLog.Infof("============================================")
		return fmt.Errorf("no default storage available (checked %d configs, %d failed)", len(configs), len(initErrors))
	}

	storageLog.Infof("[DEFAULT STORAGE] ID=%d, Name=%s", nextDefaultID, nextDefaultProvider.Name())
	storageLog.Infof("============================================")

	return nil
}

// firstEnabledProvider 返回 ID 最小的一个可写 provider。
func firstEnabledProvider(providers map[uint]providerEntry) (uint, Provider, bool) {
	var bestID uint
	var bestProvider Provider
	found := false
	for id, entry := range providers {
		if !entry.writable {
			continue
		}
		if !found || id < bestID {
			bestID = id
			bestProvider = entry.provider
			found = true
		}
	}
	return bestID, bestProvider, found
}

// GetDefault 获取默认存储提供者
func GetDefault() Provider {
	return currentRegistry().defaultProvider
}

// GetDefaultID 获取默认存储配置ID
func GetDefaultID() uint {
	return currentRegistry().defaultID
}

// GetByID 按ID获取存储提供者（读访问：无视 writable）
func GetByID(id uint) (Provider, error) {
	entry, ok := currentRegistry().providers[id]
	if !ok {
		return nil, fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderNotFound)
	}
	return entry.provider, nil
}

// GetWritableByID 返回指定 ID 的可写 provider（写访问）。
// 已加载但被禁用返回 ErrProviderDisabled；未加载返回 ErrProviderNotFound。
func GetWritableByID(id uint) (Provider, error) {
	entry, ok := currentRegistry().providers[id]
	if !ok {
		return nil, fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderNotFound)
	}
	if !entry.writable {
		return nil, fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderDisabled)
	}
	return entry.provider, nil
}

// IsWritable 报告指定 ID（0 表示当前默认）的 provider 是否可写。
func IsWritable(id uint) bool {
	state := currentRegistry()
	if id == 0 {
		return state.defaultProvider != nil
	}
	entry, ok := state.providers[id]
	return ok && entry.writable
}

// ResolveWritable 从单一 registry 快照解析当前可写 provider 与其 ID。
// id==0 解析当前默认存储；显式 ID 返回对应可写 provider。
// 禁用 → ErrProviderDisabled；未加载/无默认 → ErrProviderNotFound / ErrNoDefaultStorage。
// 上传和转换都通过它获取实际写入 provider，避免 TOCTOU 与默认来源漂移。
func ResolveWritable(id uint) (Provider, uint, error) {
	state := currentRegistry()
	if id == 0 {
		if state.defaultProvider == nil {
			return nil, 0, ErrNoDefaultStorage
		}
		return state.defaultProvider, state.defaultID, nil
	}
	entry, ok := state.providers[id]
	if !ok {
		return nil, 0, fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderNotFound)
	}
	if !entry.writable {
		return nil, 0, fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderDisabled)
	}
	return entry.provider, id, nil
}

// AddOrUpdateProvider 动态添加或更新存储提供者。writable 取自 cfg.IsEnabled。
// 仅当 cfg.IsDefault && cfg.IsEnabled 才设为默认；若更新的 ID 正是当前默认，
// 但新状态不再满足该条件，则在同一快照中清除默认指针/ID（不保留悬空默认）。
func AddOrUpdateProvider(cfg StorageConfig) error {
	provider, err := createProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	providersMu.Lock()
	defer providersMu.Unlock()

	next := cloneRegistry(currentRegistry())
	next.providers[cfg.ID] = providerEntry{provider: provider, writable: cfg.IsEnabled}

	if cfg.IsDefault && cfg.IsEnabled {
		next.defaultProvider = provider
		next.defaultID = cfg.ID
	} else if cfg.ID == next.defaultID {
		next.defaultProvider = nil
		next.defaultID = 0
	}

	registryPtr.Store(next)

	return nil
}

// SetProviderWritable 只翻 writable 标志，不重建 provider（即使凭证损坏也能禁用）。
// 若尝试把当前默认设为不可写，返回 ErrCannotDisableDefaultProvider。
// id 不存在则 no-op 返回 nil。
func SetProviderWritable(id uint, writable bool) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	state := currentRegistry()
	entry, ok := state.providers[id]
	if !ok {
		return nil
	}
	if !writable && id == state.defaultID {
		return fmt.Errorf("storage provider with ID %d: %w", id, ErrCannotDisableDefaultProvider)
	}
	next := cloneRegistry(state)
	next.providers[id] = providerEntry{provider: entry.provider, writable: writable}
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

// SetDefaultID 动态切换默认存储；目标必须已加载且可写。
func SetDefaultID(id uint) error {
	providersMu.Lock()
	defer providersMu.Unlock()

	next := cloneRegistry(currentRegistry())
	entry, ok := next.providers[id]
	if !ok {
		return fmt.Errorf("storage provider with ID %d not found", id)
	}
	if !entry.writable {
		return fmt.Errorf("storage provider with ID %d: %w", id, ErrProviderDisabled)
	}

	next.defaultProvider = entry.provider
	next.defaultID = id
	registryPtr.Store(next)
	return nil
}

// ResetForTest 清空全局 registry，仅供测试。
func ResetForTest() {
	providersMu.Lock()
	defer providersMu.Unlock()
	registryPtr.Store(&registryState{providers: make(map[uint]providerEntry)})
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
	Writable  bool
}

// ListProviders 列出所有存储提供者信息
func ListProviders() []ProviderInfo {
	state := currentRegistry()
	result := make([]ProviderInfo, 0, len(state.providers))
	for id, entry := range state.providers {
		result = append(result, ProviderInfo{
			ID:        id,
			Name:      entry.provider.Name(),
			Type:      "unknown",
			IsDefault: id == state.defaultID,
			Writable:  entry.writable,
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

	fallback := &registryState{providers: make(map[uint]providerEntry)}
	if registryPtr.CompareAndSwap(nil, fallback) {
		return fallback
	}
	return registryPtr.Load()
}

func cloneRegistry(state *registryState) *registryState {
	nextProviders := make(map[uint]providerEntry, len(state.providers))
	for id, entry := range state.providers {
		nextProviders[id] = entry
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
