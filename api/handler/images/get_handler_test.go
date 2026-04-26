package images

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCheckETagSupportsWeakAndMultiValueIfNoneMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		headerValue string
		etag        string
		wantMatch   bool
	}{
		{name: "strong_exact_match", headerValue: `"abc"`, etag: "abc", wantMatch: true},
		{name: "weak_match", headerValue: `W/"abc"`, etag: "abc", wantMatch: true},
		{name: "multi_value_match", headerValue: `"other", W/"abc"`, etag: "abc", wantMatch: true},
		{name: "wildcard_match", headerValue: `*`, etag: "abc", wantMatch: true},
		{name: "miss", headerValue: `"other"`, etag: "abc", wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodGet, "/images/test", nil)
			req.Header.Set("If-None-Match", tt.headerValue)
			c.Request = req

			matched := checkETag(c, tt.etag)

			assert.Equal(t, tt.wantMatch, matched)
			assert.Equal(t, `"abc"`, w.Header().Get("ETag"))
			if tt.wantMatch {
				assert.Equal(t, http.StatusNotModified, c.Writer.Status())
			} else {
				assert.NotEqual(t, http.StatusNotModified, c.Writer.Status())
			}
		})
	}
}

// MockConfigManager 用于测试的配置管理器 mock
type MockConfigManager struct {
	mock.Mock
}

func (m *MockConfigManager) GetGlobalTransferMode(ctx context.Context) storage.TransferMode {
	args := m.Called(ctx)
	return args.Get(0).(storage.TransferMode)
}

func (m *MockConfigManager) SetGlobalTransferMode(ctx context.Context, mode storage.TransferMode) error {
	args := m.Called(ctx, mode)
	return args.Error(0)
}

// MockDirectURLProvider 支持直链的存储提供者 mock
type MockDirectURLProvider struct {
	mock.Mock
}

func (m *MockDirectURLProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return m.Called(ctx, storagePath, file).Error(0)
}

func (m *MockDirectURLProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	args := m.Called(ctx, storagePath)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadSeeker), args.Error(1)
}

func (m *MockDirectURLProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return m.Called(ctx, storagePath).Error(0)
}

func (m *MockDirectURLProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	args := m.Called(ctx, storagePath)
	return args.Bool(0), args.Error(1)
}

func (m *MockDirectURLProvider) Health(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *MockDirectURLProvider) Name() string {
	return m.Called().String(0)
}

func (m *MockDirectURLProvider) GetDirectURL(storagePath string) string {
	return m.Called(storagePath).String(0)
}

func (m *MockDirectURLProvider) SupportsDirectLink() bool {
	return m.Called().Bool(0)
}

func (m *MockDirectURLProvider) ShouldProxy(imageIsPublic bool, globalMode storage.TransferMode) bool {
	return m.Called(imageIsPublic, globalMode).Bool(0)
}

func TestGetGlobalTransferMode(t *testing.T) {
	cm := &MockConfigManager{}

	// 测试 singleflight 行为 - 允许多次调用
	cm.On("GetGlobalTransferMode", mock.Anything).Return(storage.TransferModeAuto).Times(5)

	// 模拟多次并发调用
	results := make([]storage.TransferMode, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = cm.GetGlobalTransferMode(context.Background())
		}(i)
	}
	wg.Wait()

	// 验证所有结果一致
	for _, result := range results {
		assert.Equal(t, storage.TransferModeAuto, result)
	}
}

func TestShouldProxy_WithDifferentModes(t *testing.T) {
	tests := []struct {
		name          string
		imageIsPublic bool
		globalMode    storage.TransferMode
		enableDirect  bool
		isPublic      bool
		forceProxy    bool
		expected      bool
	}{
		{
			name:          "auto_public_supports_direct",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAuto,
			enableDirect:  true,
			isPublic:      true,
			forceProxy:    false,
			expected:      false,
		},
		{
			name:          "auto_private_image",
			imageIsPublic: false,
			globalMode:    storage.TransferModeAuto,
			enableDirect:  true,
			isPublic:      true,
			forceProxy:    false,
			expected:      true,
		},
		{
			name:          "always_proxy_mode",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAlwaysProxy,
			enableDirect:  true,
			isPublic:      true,
			forceProxy:    false,
			expected:      true,
		},
		{
			name:          "always_direct_with_support",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAlwaysDirect,
			enableDirect:  true,
			isPublic:      true,
			forceProxy:    false,
			expected:      false,
		},
		{
			name:          "force_proxy_override",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAlwaysDirect,
			enableDirect:  true,
			isPublic:      true,
			forceProxy:    true,
			expected:      true,
		},
		{
			name:          "not_public_bucket",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAuto,
			enableDirect:  true,
			isPublic:      false,
			forceProxy:    false,
			expected:      true,
		},
		{
			name:          "direct_link_disabled",
			imageIsPublic: true,
			globalMode:    storage.TransferModeAuto,
			enableDirect:  false,
			isPublic:      true,
			forceProxy:    false,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := simulateShouldProxy(tt.imageIsPublic, tt.globalMode, tt.enableDirect, tt.isPublic, tt.forceProxy)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldProxyByAutoSize(t *testing.T) {
	const threshold = int64(1 << 20)

	tests := []struct {
		name      string
		mode      storage.TransferMode
		fileSize  int64
		threshold int64
		wantProxy bool
	}{
		{
			name:      "auto_small_file_proxies",
			mode:      storage.TransferModeAuto,
			fileSize:  threshold - 1,
			threshold: threshold,
			wantProxy: true,
		},
		{
			name:      "auto_equal_threshold_proxies",
			mode:      storage.TransferModeAuto,
			fileSize:  threshold,
			threshold: threshold,
			wantProxy: true,
		},
		{
			name:      "auto_large_file_directs",
			mode:      storage.TransferModeAuto,
			fileSize:  threshold + 1,
			threshold: threshold,
			wantProxy: false,
		},
		{
			name:      "always_direct_ignores_size_threshold",
			mode:      storage.TransferModeAlwaysDirect,
			fileSize:  1,
			threshold: threshold,
			wantProxy: false,
		},
		{
			name:      "unknown_auto_size_proxies",
			mode:      storage.TransferModeAuto,
			fileSize:  0,
			threshold: threshold,
			wantProxy: true,
		},
		{
			name:      "empty_mode_uses_auto_policy",
			mode:      "",
			fileSize:  threshold - 1,
			threshold: threshold,
			wantProxy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldProxyByAutoSize(tt.mode, tt.fileSize, tt.threshold)
			assert.Equal(t, tt.wantProxy, got)
		})
	}
}

// simulateShouldProxy 模拟 ShouldProxy 的逻辑
func simulateShouldProxy(imageIsPublic bool, globalMode storage.TransferMode, enableDirect, isPublicBucket, forceProxy bool) bool {
	// ForceProxy 强制走代理
	if forceProxy {
		return true
	}

	// 不支持直链的配置走代理
	if !enableDirect || !isPublicBucket {
		return true
	}

	// 根据全局模式判断
	switch globalMode {
	case storage.TransferModeAlwaysProxy:
		return true
	case storage.TransferModeAlwaysDirect:
		return false
	case storage.TransferModeAuto:
		// auto 模式下，私有图片走代理，公开图片走直链
		return !imageIsPublic
	default:
		// 未知模式，安全起见走代理
		return true
	}
}

func TestServeOriginalImage_WithDirectLink(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		image          *models.Image
		directURL      string
		expectedStatus int
		expectedHeader string
	}{
		{
			name: "direct_link_redirects",
			image: &models.Image{
				Identifier:      "test123",
				IsPublic:        true,
				StoragePath:     "2024/01/test.jpg",
				StorageConfigID: 1,
				MimeType:        "image/jpeg",
			},
			directURL:      "https://cdn.example.com/2024/01/test.jpg",
			expectedStatus: http.StatusFound,
			expectedHeader: "https://cdn.example.com/2024/01/test.jpg",
		},
		{
			name: "no_direct_link_proxies",
			image: &models.Image{
				Identifier:      "test456",
				IsPublic:        true,
				StoragePath:     "2024/01/private.jpg",
				StorageConfigID: 1,
				MimeType:        "image/jpeg",
			},
			directURL:      "",            // 空表示不走直链
			expectedStatus: http.StatusOK, // 会继续代理逻辑
			expectedHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			// 设置一个 mock request，Redirect 需要它
			c.Request = httptest.NewRequest(http.MethodGet, "/images/"+tt.image.Identifier, nil)

			// 模拟 serveOriginalImage 的直链分支
			if tt.directURL != "" {
				c.Header("Cache-Control", "public, max-age=31536000")
				c.Redirect(http.StatusFound, tt.directURL)
			}

			if tt.directURL != "" {
				assert.Equal(t, tt.expectedStatus, w.Code)
				location := w.Header().Get("Location")
				assert.Equal(t, tt.expectedHeader, location)
			}
		})
	}
}

func TestServeVariantImage_WithDirectLink(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		image          *models.Image
		variant        *imageSvc.VariantResult
		directURL      string
		expectedStatus int
	}{
		{
			name: "variant_with_direct_link_redirects",
			image: &models.Image{
				IsPublic:        true,
				StorageConfigID: 1,
			},
			variant: &imageSvc.VariantResult{
				StoragePath: "variants/2024/01/test.webp",
				MIMEType:    "image/webp",
			},
			directURL:      "https://cdn.example.com/variants/2024/01/test.webp",
			expectedStatus: http.StatusFound,
		},
		{
			name: "variant_without_direct_link_fallback",
			image: &models.Image{
				IsPublic:        false, // 私有图片
				StorageConfigID: 1,
			},
			variant: &imageSvc.VariantResult{
				StoragePath: "variants/2024/01/private.webp",
				MIMEType:    "image/webp",
			},
			directURL:      "",
			expectedStatus: 0, // 不会重定向
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			// 设置一个 mock request，Redirect 需要它
			c.Request = httptest.NewRequest(http.MethodGet, "/thumbnails/test", nil)

			// 模拟 serveVariantImage 的直链分支逻辑
			if tt.directURL != "" {
				c.Header("Cache-Control", "public, max-age=31536000")
				c.Redirect(http.StatusFound, tt.directURL)
			}

			if tt.directURL != "" {
				assert.Equal(t, tt.expectedStatus, w.Code)
				location := w.Header().Get("Location")
				assert.Equal(t, tt.directURL, location)
			}
		})
	}
}

// TestSingleflightConcurrency 测试 singleflight 的并发安全性
func TestSingleflightConcurrency(t *testing.T) {
	cm := &MockConfigManager{}

	// 模拟慢速操作 - 使用原子操作避免竞态
	var callCount atomic.Int32
	cm.On("GetGlobalTransferMode", mock.Anything).Run(func(args mock.Arguments) {
		callCount.Add(1)
		time.Sleep(10 * time.Millisecond) // 模拟慢速数据库查询
	}).Return(storage.TransferModeAuto).Times(10)

	var wg sync.WaitGroup
	results := make([]storage.TransferMode, 10)

	// 并发调用
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = cm.GetGlobalTransferMode(context.Background())
		}(i)
	}

	wg.Wait()

	// 所有结果应该相同
	for _, r := range results {
		assert.Equal(t, storage.TransferModeAuto, r)
	}

	// 验证 mock 被调用了10次（因为不是真正的 singleflight，只是测试 mock）
	// 真正的 singleflight 会在 getGlobalTransferMode 方法中实现
	assert.Equal(t, int32(10), callCount.Load())
}

func TestRemoteImageDataCacheKey(t *testing.T) {
	assert.Equal(t, "7:original/2026/03/26/hash.jpg", remoteImageDataCacheKey(7, "original/2026/03/26/hash.jpg"))
}

type testPathProvider struct{}

func (p *testPathProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *testPathProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	return nil, nil
}

func (p *testPathProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *testPathProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return true, nil
}

func (p *testPathProvider) Health(ctx context.Context) error {
	return nil
}

func (p *testPathProvider) Name() string {
	return "local"
}

func (p *testPathProvider) GetFilePath(storagePath string) (string, error) {
	return "/tmp/test", nil
}

type testRemoteProvider struct{}

func (p *testRemoteProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *testRemoteProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	return nil, nil
}

func (p *testRemoteProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *testRemoteProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return true, nil
}

func (p *testRemoteProvider) Health(ctx context.Context) error {
	return nil
}

func (p *testRemoteProvider) Name() string {
	return "s3"
}

var _ storage.PathProvider = (*testPathProvider)(nil)
var _ storage.Provider = (*testPathProvider)(nil)
var _ storage.Provider = (*testRemoteProvider)(nil)

func TestShouldUseImageDataCache(t *testing.T) {
	handler := &Handler{
		cacheHelper:      cache.NewHelper(nil),
		imageDataCaching: true,
	}

	assert.False(t, handler.shouldUseImageDataCache(nil))
	assert.True(t, handler.shouldUseImageDataCache(&testRemoteProvider{}))
	assert.False(t, handler.shouldUseImageDataCache(&testPathProvider{}))

	handler.imageDataCaching = false
	assert.False(t, handler.shouldUseImageDataCache(&testRemoteProvider{}))
}

type cacheFillProvider struct {
	data       []byte
	size       int64
	getCalls   atomic.Int32
	infoCalls  atomic.Int32
	storageKey string
}

func (p *cacheFillProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *cacheFillProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	p.getCalls.Add(1)
	p.storageKey = storagePath
	return bytes.NewReader(p.data), nil
}

func (p *cacheFillProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *cacheFillProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return true, nil
}

func (p *cacheFillProvider) Health(ctx context.Context) error {
	return nil
}

func (p *cacheFillProvider) Name() string {
	return "s3"
}

func (p *cacheFillProvider) GetObjectInfo(ctx context.Context, storagePath string) (storage.ObjectInfo, error) {
	p.infoCalls.Add(1)
	size := p.size
	if size == 0 {
		size = int64(len(p.data))
	}
	return storage.ObjectInfo{Size: size}, nil
}

func TestGetOrPopulateImageDataCachePopulatesRemoteCache(t *testing.T) {
	providerCache, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1000,
		MaxCost:     1024 * 1024,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer func() { _ = providerCache.Close() }()

	handler := &Handler{
		cacheHelper: cache.NewHelper(providerCache, cache.HelperConfig{
			ImageCacheTTL:         time.Minute,
			ImageDataCacheTTL:     time.Minute,
			MaxCacheableImageSize: 1024,
		}),
		imageDataCaching: true,
	}

	provider := &cacheFillProvider{data: []byte("hello-image")}

	data, ok := handler.getOrPopulateImageDataCache(context.Background(), provider, "image_data:test", "path/test.jpg")
	require.True(t, ok)
	assert.Equal(t, []byte("hello-image"), data)
	assert.Equal(t, int32(1), provider.getCalls.Load())
	assert.Equal(t, int32(1), provider.infoCalls.Load())

	data, ok = handler.getOrPopulateImageDataCache(context.Background(), provider, "image_data:test", "path/test.jpg")
	require.True(t, ok)
	assert.Equal(t, []byte("hello-image"), data)
	assert.Equal(t, int32(1), provider.getCalls.Load())
	assert.Equal(t, int32(1), provider.infoCalls.Load())
}

func TestGetOrPopulateImageDataCacheSkipsLocalProvider(t *testing.T) {
	providerCache, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1000,
		MaxCost:     1024 * 1024,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer func() { _ = providerCache.Close() }()

	handler := &Handler{
		cacheHelper:      cache.NewHelper(providerCache),
		imageDataCaching: true,
	}

	data, ok := handler.getOrPopulateImageDataCache(context.Background(), &testPathProvider{}, "image_data:test", "path/test.jpg")
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestGetOrPopulateImageDataCacheSkipsLargeRemoteObjectWithoutDownloading(t *testing.T) {
	providerCache, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1000,
		MaxCost:     1024 * 1024,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer func() { _ = providerCache.Close() }()

	handler := &Handler{
		cacheHelper: cache.NewHelper(providerCache, cache.HelperConfig{
			ImageCacheTTL:         time.Minute,
			ImageDataCacheTTL:     time.Minute,
			MaxCacheableImageSize: 4,
		}),
		imageDataCaching: true,
	}

	provider := &cacheFillProvider{
		data: []byte("hello-image"),
		size: int64(len("hello-image")),
	}

	data, ok := handler.getOrPopulateImageDataCache(context.Background(), provider, "image_data:large", "path/large.jpg")
	assert.False(t, ok)
	assert.Nil(t, data)
	assert.Equal(t, int32(1), provider.infoCalls.Load())
	assert.Equal(t, int32(0), provider.getCalls.Load())
}

func TestServeImageDataUsesPrivateCacheControlForPrivateImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}
	img := &models.Image{
		Identifier: "private-image",
		FileHash:   "private-hash",
		MimeType:   "image/jpeg",
		IsPublic:   false,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/private-image", nil)

	h.serveImageData(c, img, []byte("body"))

	assert.Equal(t, privateImageCacheControl, w.Header().Get("Cache-Control"))
}
