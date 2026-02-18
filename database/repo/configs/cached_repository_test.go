package configs

import (
	"context"
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/cache/memory"
	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// MockRepository 模拟配置仓库
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, config *models.SystemConfig) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockRepository) Update(ctx context.Context, config *models.SystemConfig) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockRepository) Delete(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) GetByID(ctx context.Context, id uint) (*models.SystemConfig, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SystemConfig), args.Error(1)
}

func (m *MockRepository) GetByKey(ctx context.Context, key string) (*models.SystemConfig, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SystemConfig), args.Error(1)
}

func (m *MockRepository) List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error) {
	args := m.Called(ctx, category, enabledOnly)
	return args.Get(0).([]models.SystemConfig), args.Error(1)
}

func (m *MockRepository) ListAll(ctx context.Context) ([]models.SystemConfig, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.SystemConfig), args.Error(1)
}

func (m *MockRepository) GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error) {
	args := m.Called(ctx, category)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SystemConfig), args.Error(1)
}

func (m *MockRepository) SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error {
	args := m.Called(ctx, id, category)
	return args.Error(0)
}

func (m *MockRepository) Enable(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) Disable(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) Count(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRepository) CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error) {
	args := m.Called(ctx, category)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRepository) Exists(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	args := m.Called(ctx, fn)
	return args.Error(0)
}

func (m *MockRepository) EnsureKeyUnique(ctx context.Context, baseKey string) (string, error) {
	args := m.Called(ctx, baseKey)
	return args.String(0), args.Error(1)
}

// setupTest 创建测试环境
func setupTest() (*CachedRepository, *MockRepository, cache.Provider) {
	mockRepo := new(MockRepository)
	memCache, _ := memory.NewMemory(memory.Config{
		NumCounters: 10000,
		MaxCost:     10485760, // 10MB
		BufferItems: 64,
		Metrics:     false,
	})
	cachedRepo := NewCachedRepository(mockRepo, memCache, time.Minute)
	return cachedRepo, mockRepo, memCache
}

func TestCachedRepository_GetDefaultByCategory_CacheHit(t *testing.T) {
	cachedRepo, mockRepo, _ := setupTest()
	ctx := context.Background()
	category := models.ConfigCategoryThumbnail

	// 准备测试数据
	config := &models.SystemConfig{
		ID:        1,
		Category:  category,
		Name:      "Test Config",
		Key:       "test:config",
		IsEnabled: true,
		IsDefault: true,
	}

	// 第一次调用，缓存未命中
	mockRepo.On("GetDefaultByCategory", ctx, category).Return(config, nil).Once()

	result1, err := cachedRepo.GetDefaultByCategory(ctx, category)
	assert.NoError(t, err)
	assert.Equal(t, config.ID, result1.ID)

	// 第二次调用，应该命中缓存，不会访问 mockRepo
	result2, err := cachedRepo.GetDefaultByCategory(ctx, category)
	assert.NoError(t, err)
	assert.Equal(t, config.ID, result2.ID)

	mockRepo.AssertExpectations(t)
}

func TestCachedRepository_GetDefaultByCategory_CacheMiss(t *testing.T) {
	cachedRepo, mockRepo, _ := setupTest()
	ctx := context.Background()
	category := models.ConfigCategoryConversion

	// 数据库中不存在配置
	mockRepo.On("GetDefaultByCategory", ctx, category).Return(nil, gorm.ErrRecordNotFound).Once()

	result, err := cachedRepo.GetDefaultByCategory(ctx, category)
	assert.Error(t, err)
	assert.Nil(t, result)

	mockRepo.AssertExpectations(t)
}

func TestCachedRepository_Create_InvalidatesCache(t *testing.T) {
	cachedRepo, mockRepo, _ := setupTest()
	ctx := context.Background()
	category := models.ConfigCategoryThumbnail

	newConfig := &models.SystemConfig{
		ID:        2,
		Category:  category,
		Name:      "New Config",
		Key:       "new:config",
		IsEnabled: true,
	}

	mockRepo.On("Create", ctx, newConfig).Return(nil).Once()

	err := cachedRepo.Create(ctx, newConfig)
	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestCachedRepository_Update_InvalidatesCache(t *testing.T) {
	cachedRepo, mockRepo, _ := setupTest()
	ctx := context.Background()
	category := models.ConfigCategoryThumbnail

	config := &models.SystemConfig{
		ID:        1,
		Category:  category,
		Name:      "Updated Config",
		Key:       "test:config",
		IsEnabled: true,
	}

	mockRepo.On("Update", ctx, config).Return(nil).Once()

	err := cachedRepo.Update(ctx, config)
	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestCachedRepository_Delete_InvalidatesCache(t *testing.T) {
	cachedRepo, mockRepo, _ := setupTest()
	ctx := context.Background()

	config := &models.SystemConfig{
		ID:        1,
		Category:  models.ConfigCategoryThumbnail,
		Name:      "Test Config",
		Key:       "test:config",
		IsEnabled: true,
	}

	mockRepo.On("GetByID", ctx, uint(1)).Return(config, nil).Once()
	mockRepo.On("Delete", ctx, uint(1)).Return(nil).Once()

	err := cachedRepo.Delete(ctx, 1)
	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		format   string
		args     []interface{}
		expected string
	}{
		{"config:id:%d", []interface{}{1}, "config:id:1"},
		{"config:key:%s", []interface{}{"test:key"}, "config:key:test:key"},
		{"config:default:%s", []interface{}{models.ConfigCategoryThumbnail}, "config:default:thumbnail"},
	}

	for _, tt := range tests {
		result := cacheKey(tt.format, tt.args...)
		assert.Equal(t, tt.expected, result)
	}
}

func TestCachedConfig_ToSystemConfig(t *testing.T) {
	cached := &CachedConfig{
		ID:          1,
		Category:    models.ConfigCategoryThumbnail,
		Name:        "Test",
		Key:         "test:key",
		IsEnabled:   true,
		IsDefault:   true,
		Priority:    10,
		Description: "Test description",
		CreatedBy:   0,
	}

	config := cached.ToSystemConfig()

	assert.Equal(t, cached.ID, config.ID)
	assert.Equal(t, cached.Category, config.Category)
	assert.Equal(t, cached.Name, config.Name)
	assert.Equal(t, cached.Key, config.Key)
	assert.Equal(t, cached.IsEnabled, config.IsEnabled)
	assert.Equal(t, cached.IsDefault, config.IsDefault)
	assert.Equal(t, cached.Priority, config.Priority)
	assert.Equal(t, cached.Description, config.Description)
}

func TestFromSystemConfig(t *testing.T) {
	config := &models.SystemConfig{
		ID:          1,
		Category:    models.ConfigCategoryConversion,
		Name:        "Test Config",
		Key:         "test:config",
		ConfigJSON:  `{"enabled":true,"quality":85}`,
		IsEnabled:   true,
		IsDefault:   false,
		Priority:    5,
		Description: "Test",
	}

	cached := FromSystemConfig(config)

	assert.Equal(t, config.ID, cached.ID)
	assert.Equal(t, config.Category, cached.Category)
	assert.Equal(t, config.Name, cached.Name)
	assert.Equal(t, config.ConfigJSON, cached.ConfigJSON)
}