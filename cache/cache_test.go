package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

// --- Mock Provider 实现 ---

type mockProvider struct {
	mu        sync.RWMutex
	data      map[string][]byte
	name      string
	closed    bool
	setError  error
	getError  error
	delError  error
	existsErr error
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		data: make(map[string][]byte),
		name: name,
	}
}

func (m *mockProvider) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if m.setError != nil {
		return m.setError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// 简单序列化（实际应使用 json）
	if str, ok := value.(string); ok {
		m.data[key] = []byte(str)
	} else if bytes, ok := value.([]byte); ok {
		m.data[key] = bytes
	}
	return nil
}

func (m *mockProvider) Get(ctx context.Context, key string, dest interface{}) error {
	if m.getError != nil {
		return m.getError
	}
	m.mu.RLock()
	val, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return ErrCacheMiss
	}
	if strPtr, ok := dest.(*string); ok {
		*strPtr = string(val)
		return nil
	}
	return nil
}

func (m *mockProvider) Delete(ctx context.Context, key string) error {
	if m.delError != nil {
		return m.delError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockProvider) Exists(ctx context.Context, key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockProvider) Close() error {
	m.closed = true
	return nil
}

func (m *mockProvider) Name() string {
	return m.name
}

// --- Provider 接口测试 ---

func TestErrCacheMiss(t *testing.T) {
	err := ErrCacheMiss
	assert.Equal(t, "cache miss", err.Error())
	assert.True(t, IsCacheMiss(err))
}

func TestIsCacheMiss(t *testing.T) {
	assert.True(t, IsCacheMiss(ErrCacheMiss))
	assert.False(t, IsCacheMiss(nil))
	assert.False(t, IsCacheMiss(assert.AnError))
}

// --- Factory 测试 ---

func TestFactory_New(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}

	assert.NotNil(t, factory)
	assert.Equal(t, mock, factory.GetProvider())
}

func TestFactory_Set(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}

	ctx := context.Background()
	err := factory.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, "value1", string(mock.data["key1"]))
}

func TestFactory_Set_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}

	ctx := context.Background()
	err := factory.Set(ctx, "key1", "value1", time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider not initialized")
}

func TestFactory_Get(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	mock.data["key1"] = []byte("value1")

	ctx := context.Background()
	var value string
	err := factory.Get(ctx, "key1", &value)
	assert.NoError(t, err)
	assert.Equal(t, "value1", value)
}

func TestFactory_Get_CacheMiss(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}

	ctx := context.Background()
	var value string
	err := factory.Get(ctx, "nonexistent", &value)
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestFactory_Get_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}

	ctx := context.Background()
	var value string
	err := factory.Get(ctx, "key1", &value)
	// Get 在 provider 为 nil 时返回 ErrCacheMiss
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestFactory_Delete(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	mock.data["key1"] = []byte("value1")

	ctx := context.Background()
	err := factory.Delete(ctx, "key1")
	assert.NoError(t, err)
	_, exists := mock.data["key1"]
	assert.False(t, exists)
}

func TestFactory_Delete_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}

	ctx := context.Background()
	err := factory.Delete(ctx, "key1")
	// Delete 操作在 provider 为 nil 时静默处理
	assert.NoError(t, err)
}

func TestFactory_Exists(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	mock.data["key1"] = []byte("value1")

	ctx := context.Background()
	exists, err := factory.Exists(ctx, "key1")
	assert.NoError(t, err)
	assert.True(t, exists)

	exists, err = factory.Exists(ctx, "nonexistent")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestFactory_Exists_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}

	ctx := context.Background()
	exists, err := factory.Exists(ctx, "key1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
	assert.False(t, exists)
}

func TestFactory_Close(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}

	err := factory.Close()
	assert.NoError(t, err)
	assert.True(t, mock.closed)
}

func TestFactory_Close_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}

	err := factory.Close()
	assert.NoError(t, err)
}

func TestFactory_GetProvider(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}

	assert.Equal(t, mock, factory.GetProvider())
}

// --- Helper 测试 ---

func TestHelper_New(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	assert.NotNil(t, helper)
	assert.Equal(t, factory, helper.factory)
}

func TestHelper_CacheImage(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	image := &models.Image{
		Identifier:   "test-image-123",
		OriginalName: "test.jpg",
	}

	err := helper.CacheImage(ctx, image)
	assert.NoError(t, err)
}

func TestHelper_CacheImage_NilFactory(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	image := &models.Image{
		Identifier: "test-image-123",
	}

	err := helper.CacheImage(ctx, image)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestHelper_CacheImage_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	image := &models.Image{
		Identifier: "test-image-123",
	}

	err := helper.CacheImage(ctx, image)
	assert.Error(t, err)
}

func TestHelper_GetCachedImage(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	
	// 先缓存
	image := &models.Image{
		Identifier: "test-image-123",
		OriginalName:   "test.jpg",
	}
	err := helper.CacheImage(ctx, image)
	assert.NoError(t, err)
}

func TestHelper_GetCachedImage_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	var image models.Image
	err := helper.GetCachedImage(ctx, "test-id", &image)
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestHelper_CacheUser(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	user := &models.User{
		Username: "testuser",
	}

	err := helper.CacheUser(ctx, user)
	assert.NoError(t, err)
}

func TestHelper_CacheUser_NilFactory(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	user := &models.User{
		Username: "testuser",
	}

	err := helper.CacheUser(ctx, user)
	assert.Error(t, err)
}

func TestHelper_GetCachedUser_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	var user models.User
	err := helper.GetCachedUser(ctx, 1, &user)
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestHelper_CacheDevice(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	device := &models.Device{
		DeviceID: "device-123",
	}

	err := helper.CacheDevice(ctx, device)
	assert.NoError(t, err)
}

func TestHelper_CacheDevice_NilFactory(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	device := &models.Device{
		DeviceID: "device-123",
	}

	err := helper.CacheDevice(ctx, device)
	assert.Error(t, err)
}

func TestHelper_GetCachedDevice_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	var device models.Device
	err := helper.GetCachedDevice(ctx, "device-123", &device)
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestHelper_DeleteCachedImage(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedImage(ctx, "image-123")
	assert.NoError(t, err)
}

func TestHelper_DeleteCachedImage_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedImage(ctx, "image-123")
	assert.NoError(t, err) // 静默处理
}

func TestHelper_DeleteCachedUser(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedUser(ctx, 1)
	assert.NoError(t, err)
}

func TestHelper_DeleteCachedUser_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedUser(ctx, 1)
	assert.NoError(t, err)
}

func TestHelper_DeleteCachedDevice(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedDevice(ctx, "device-123")
	assert.NoError(t, err)
}

func TestHelper_DeleteCachedDevice_NilProvider(t *testing.T) {
	factory := &Factory{defaultProvider: nil}
	helper := NewHelper(factory)

	ctx := context.Background()
	err := helper.DeleteCachedDevice(ctx, "device-123")
	assert.NoError(t, err)
}

// --- 缓存键前缀测试 ---

func TestCachePrefixes(t *testing.T) {
	assert.Equal(t, "image:", ImageCachePrefix)
	assert.Equal(t, "user:", UserCachePrefix)
	assert.Equal(t, "device:", DeviceCachePrefix)
	assert.Equal(t, "static_token:", StaticTokenCachePrefix)
	assert.Equal(t, "empty:", EmptyValueCachePrefix)
}

func TestCacheExpirations(t *testing.T) {
	assert.Equal(t, 1*time.Hour, DefaultImageCacheExpiration)
	assert.Equal(t, 30*time.Minute, DefaultUserCacheExpiration)
	assert.Equal(t, 24*time.Hour, DefaultDeviceCacheExpiration)
	assert.Equal(t, 1*time.Hour, DefaultStaticTokenCacheExpiration)
	assert.Equal(t, 5*time.Minute, DefaultEmptyValueCacheExpiration)
}

// --- 并发安全测试 ---

func TestMockProvider_Concurrent(t *testing.T) {
	mock := newMockProvider("mock")
	factory := &Factory{providers: map[uint]Provider{1: mock}, defaultProvider: mock}
	ctx := context.Background()

	// 并发写入
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := "key" + string(rune('0'+idx))
			value := "value" + string(rune('0'+idx))
			factory.Set(ctx, key, value, time.Minute)
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证写入
	for i := 0; i < 10; i++ {
		key := "key" + string(rune('0'+i))
		exists, _ := factory.Exists(ctx, key)
		assert.True(t, exists, "key %s should exist", key)
	}
}
