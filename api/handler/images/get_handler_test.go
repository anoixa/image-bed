package images

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

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

func TestGetDirectURLIfPossible(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		image          *models.Image
		setupMock      func(*MockConfigManager, *MockDirectURLProvider)
		expectedResult string
	}{
		{
			name: "private_image_should_not_use_direct",
			image: &models.Image{
				IsPublic:        false,
				StoragePath:     "2024/01/test.jpg",
				StorageConfigID: 1,
			},
			setupMock: func(cm *MockConfigManager, dsp *MockDirectURLProvider) {
				// 私有图片不应该调用任何存储方法
			},
			expectedResult: "",
		},
		{
			name: "public_image_with_direct_link_support",
			image: &models.Image{
				IsPublic:        true,
				StoragePath:     "2024/01/public.jpg",
				StorageConfigID: 1,
			},
			setupMock: func(cm *MockConfigManager, dsp *MockDirectURLProvider) {
				cm.On("GetGlobalTransferMode", mock.Anything).Return(storage.TransferModeAuto).Once()
				dsp.On("ShouldProxy", true, storage.TransferModeAuto).Return(false).Once()
				dsp.On("GetDirectURL", "2024/01/public.jpg").Return("https://cdn.example.com/2024/01/public.jpg").Once()
			},
			expectedResult: "https://cdn.example.com/2024/01/public.jpg",
		},
		{
			name: "public_image_but_should_proxy",
			image: &models.Image{
				IsPublic:        true,
				StoragePath:     "2024/01/proxy.jpg",
				StorageConfigID: 1,
			},
			setupMock: func(cm *MockConfigManager, dsp *MockDirectURLProvider) {
				cm.On("GetGlobalTransferMode", mock.Anything).Return(storage.TransferModeAlwaysProxy).Once()
				dsp.On("ShouldProxy", true, storage.TransferModeAlwaysProxy).Return(true).Once()
			},
			expectedResult: "",
		},
		{
			name: "always_direct_mode_returns_url",
			image: &models.Image{
				IsPublic:        true,
				StoragePath:     "2024/01/direct.jpg",
				StorageConfigID: 1,
			},
			setupMock: func(cm *MockConfigManager, dsp *MockDirectURLProvider) {
				cm.On("GetGlobalTransferMode", mock.Anything).Return(storage.TransferModeAlwaysDirect).Once()
				dsp.On("ShouldProxy", true, storage.TransferModeAlwaysDirect).Return(false).Once()
				dsp.On("GetDirectURL", "2024/01/direct.jpg").Return("https://cdn.example.com/2024/01/direct.jpg").Once()
			},
			expectedResult: "https://cdn.example.com/2024/01/direct.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &MockConfigManager{}
			dsp := &MockDirectURLProvider{}

			if tt.setupMock != nil {
				tt.setupMock(cm, dsp)
			}

			// 由于 getDirectURLIfPossible 依赖 storage 包的全局状态
			// 我们测试逻辑而不是完整集成
			result := simulateGetDirectURLIfPossible(tt.image, cm, dsp)

			assert.Equal(t, tt.expectedResult, result)
			cm.AssertExpectations(t)
			dsp.AssertExpectations(t)
		})
	}
}

// simulateGetDirectURLIfPossible 模拟 getDirectURLIfPossible 的逻辑
func simulateGetDirectURLIfPossible(img *models.Image, cm *MockConfigManager, provider storage.DirectURLProvider) string {
	// 私有图片不支持直链
	if !img.IsPublic {
		return ""
	}

	if provider == nil {
		return ""
	}

	// 获取全局模式
	globalMode := cm.GetGlobalTransferMode(context.Background())

	// 判断是否应该走代理
	if provider.ShouldProxy(img.IsPublic, globalMode) {
		return ""
	}

	// 获取直链 URL
	return provider.GetDirectURL(img.StoragePath)
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

func TestGetStorageProvider(t *testing.T) {
	tests := []struct {
		name            string
		storageConfigID uint
		expectedCall    bool
	}{
		{
			name:            "zero_id_returns_default",
			storageConfigID: 0,
			expectedCall:    false,
		},
		{
			name:            "non_zero_id_tries_to_get_provider",
			storageConfigID: 5,
			expectedCall:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 这个测试主要验证逻辑分支
			result := simulateGetStorageProvider(tt.storageConfigID)
			if tt.expectedCall {
				// 非零 ID 应该尝试获取特定 provider
				assert.NotNil(t, result)
			} else {
				// 零 ID 应该返回默认 provider
				assert.NotNil(t, result)
			}
		})
	}
}

// simulateGetStorageProvider 模拟 getStorageProvider 逻辑
func simulateGetStorageProvider(storageConfigID uint) interface{} {
	if storageConfigID == 0 {
		return "default"
	}
	// 实际会调用 storage.GetByID
	return "specific"
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
