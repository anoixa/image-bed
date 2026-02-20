package cache

import (
	"context"
	"encoding/json"
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
	if str, ok := value.(string); ok {
		m.data[key] = []byte(str)
	} else if bytes, ok := value.([]byte); ok {
		m.data[key] = bytes
	} else {
		// 序列化其他类型
		data, _ := json.Marshal(value)
		m.data[key] = data
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
	// 反序列化到目标对象
	return json.Unmarshal(val, dest)
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

// --- Helper 测试 ---

func TestHelper_New(t *testing.T) {
	mock := newMockProvider("mock")
	helper := NewHelper(mock)

	assert.NotNil(t, helper)
}

func TestHelper_CacheImage(t *testing.T) {
	mock := newMockProvider("mock")
	helper := NewHelper(mock)

	ctx := context.Background()
	image := &models.Image{
		Identifier:   "test-image-123",
		OriginalName: "test.jpg",
	}

	err := helper.CacheImage(ctx, image)
	assert.NoError(t, err)
}

func TestHelper_CacheImage_NilProvider(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	image := &models.Image{
		Identifier:   "test-image-123",
		OriginalName: "test.jpg",
	}

	err := helper.CacheImage(ctx, image)
	assert.Error(t, err)
}

func TestHelper_GetCachedImage(t *testing.T) {
	mock := newMockProvider("mock")
	helper := NewHelper(mock)

	ctx := context.Background()
	image := &models.Image{
		Identifier:   "test-image-123",
		OriginalName: "test.jpg",
	}

	err := helper.CacheImage(ctx, image)
	assert.NoError(t, err)

	var cachedImage models.Image
	err = helper.GetCachedImage(ctx, "test-image-123", &cachedImage)
	assert.NoError(t, err)
}

func TestHelper_GetCachedImage_NilProvider(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	var cachedImage models.Image
	err := helper.GetCachedImage(ctx, "test-image-123", &cachedImage)
	assert.Error(t, err)
	assert.True(t, IsCacheMiss(err))
}

func TestHelper_DeleteCachedImage(t *testing.T) {
	mock := newMockProvider("mock")
	helper := NewHelper(mock)

	ctx := context.Background()
	err := helper.DeleteCachedImage(ctx, "test-image-123")
	assert.NoError(t, err)
}

func TestHelper_DeleteCachedImage_NilProvider(t *testing.T) {
	helper := NewHelper(nil)

	ctx := context.Background()
	err := helper.DeleteCachedImage(ctx, "test-image-123")
	assert.NoError(t, err)
}

// --- Utils 测试 ---

func TestAddJitter(t *testing.T) {
	duration := 1 * time.Hour

	// 测试正数 duration
	result := addJitter(duration)
	assert.True(t, result >= duration)
	assert.True(t, result <= duration+duration/10)

	// 测试零 duration
	zeroResult := addJitter(0)
	assert.Equal(t, time.Duration(0), zeroResult)

	// 测试负数 duration
	negativeDuration := -1 * time.Hour
	negativeResult := addJitter(negativeDuration)
	assert.Equal(t, negativeDuration, negativeResult)
}
