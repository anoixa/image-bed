package configs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// CacheTTL 默认缓存过期时间
const DefaultCacheTTL = 5 * time.Minute

// cacheKey 构建缓存键
func cacheKey(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

// CachedRepository 带缓存的配置仓库装饰器
type CachedRepository struct {
	repo  Repository
	cache cache.Provider
	ttl   time.Duration
}

// CachedConfig 缓存的配置数据结构
type CachedConfig struct {
	ID          uint                   `json:"id"`
	Category    models.ConfigCategory  `json:"category"`
	Name        string                 `json:"name"`
	Key         string                 `json:"key"`
	IsEnabled   bool                   `json:"is_enabled"`
	IsDefault   bool                   `json:"is_default"`
	Priority    int                    `json:"priority"`
	ConfigJSON  string                 `json:"config_json"` // 加密后的配置 JSON
	Description string                 `json:"description"`
	CreatedBy   uint                   `json:"created_by"`
}

// ToSystemConfig 转换为 SystemConfig 模型
func (c *CachedConfig) ToSystemConfig() *models.SystemConfig {
	return &models.SystemConfig{
		ID:          c.ID,
		Category:    c.Category,
		Name:        c.Name,
		Key:         c.Key,
		IsEnabled:   c.IsEnabled,
		IsDefault:   c.IsDefault,
		Priority:    c.Priority,
		ConfigJSON:  c.ConfigJSON, // 返回加密的配置 JSON
		Description: c.Description,
		CreatedBy:   c.CreatedBy,
	}
}

// FromSystemConfig 从 SystemConfig 创建缓存数据
func FromSystemConfig(config *models.SystemConfig) *CachedConfig {
	return &CachedConfig{
		ID:          config.ID,
		Category:    config.Category,
		Name:        config.Name,
		Key:         config.Key,
		IsEnabled:   config.IsEnabled,
		IsDefault:   config.IsDefault,
		Priority:    config.Priority,
		ConfigJSON:  config.ConfigJSON, // 存储加密的配置 JSON
		Description: config.Description,
		CreatedBy:   config.CreatedBy,
	}
}

// NewCachedRepository 创建带缓存的配置仓库
func NewCachedRepository(repo Repository, cache cache.Provider, ttl time.Duration) *CachedRepository {
	if ttl == 0 {
		ttl = DefaultCacheTTL
	}
	return &CachedRepository{
		repo:  repo,
		cache: cache,
		ttl:   ttl,
	}
}

// Create 创建配置（同时清除相关缓存）
func (c *CachedRepository) Create(ctx context.Context, config *models.SystemConfig) error {
	if err := c.repo.Create(ctx, config); err != nil {
		return err
	}
	// 清除该分类的默认配置缓存
	c.InvalidateByCategory(ctx, config.Category)
	return nil
}

// Update 更新配置（同时清除缓存）
func (c *CachedRepository) Update(ctx context.Context, config *models.SystemConfig) error {
	if err := c.repo.Update(ctx, config); err != nil {
		return err
	}
	// 清除该配置的所有缓存
	c.InvalidateByID(ctx, config.ID)
	c.InvalidateByKey(ctx, config.Key)
	c.InvalidateByCategory(ctx, config.Category)
	return nil
}

// Delete 删除配置（同时清除缓存）
func (c *CachedRepository) Delete(ctx context.Context, id uint) error {
	// 先获取配置信息，用于清除缓存
	config, err := c.repo.GetByID(ctx, id)
	if err == nil {
		c.InvalidateByID(ctx, id)
		c.InvalidateByKey(ctx, config.Key)
		c.InvalidateByCategory(ctx, config.Category)
	}
	return c.repo.Delete(ctx, id)
}

// GetByID 根据ID获取配置（带缓存）
func (c *CachedRepository) GetByID(ctx context.Context, id uint) (*models.SystemConfig, error) {
	cacheKey := cacheKey("config:id:%d", id)

	var cached CachedConfig
	if err := c.cache.Get(ctx, cacheKey, &cached); err == nil {
		log.Printf("[CachedRepository] Cache hit for config ID: %d", id)
		return cached.ToSystemConfig(), nil
	}

	// 缓存未命中，查库
	config, err := c.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 不缓存加密数据，只缓存元数据标记
	// 实际配置内容在获取时会解密
	return config, nil
}

// GetByKey 根据Key获取配置（带缓存）
func (c *CachedRepository) GetByKey(ctx context.Context, key string) (*models.SystemConfig, error) {
	cacheKey := cacheKey("config:key:%s", key)

	var cached CachedConfig
	if err := c.cache.Get(ctx, cacheKey, &cached); err == nil {
		log.Printf("[CachedRepository] Cache hit for config key: %s", key)
		return cached.ToSystemConfig(), nil
	}

	return c.repo.GetByKey(ctx, key)
}

// List 列出配置（带缓存，短时间缓存列表）
func (c *CachedRepository) List(ctx context.Context, category models.ConfigCategory, enabledOnly bool) ([]models.SystemConfig, error) {
	cacheKey := cacheKey("config:list:%s:%v", category, enabledOnly)

	var cached []CachedConfig
	if err := c.cache.Get(ctx, cacheKey, &cached); err == nil {
		log.Printf("[CachedRepository] Cache hit for config list: category=%s", category)
		configs := make([]models.SystemConfig, len(cached))
		for i, c := range cached {
			configs[i] = *c.ToSystemConfig()
		}
		return configs, nil
	}

	configs, err := c.repo.List(ctx, category, enabledOnly)
	if err != nil {
		return nil, err
	}

	// 缓存列表（短时间缓存，如1分钟）
	cached = make([]CachedConfig, len(configs))
	for i, config := range configs {
		cached[i] = *FromSystemConfig(&config)
	}
	if err := c.cache.Set(ctx, cacheKey, cached, time.Minute); err != nil {
		log.Printf("[CachedRepository] Failed to cache list: %v", err)
	}

	return configs, nil
}

// ListAll 列出所有配置
func (c *CachedRepository) ListAll(ctx context.Context) ([]models.SystemConfig, error) {
	return c.List(ctx, "", false)
}

// GetDefaultByCategory 获取默认配置（带缓存）- 这是最主要的缓存场景
func (c *CachedRepository) GetDefaultByCategory(ctx context.Context, category models.ConfigCategory) (*models.SystemConfig, error) {
	cacheKey := cacheKey("config:default:%s", category)

	var cached CachedConfig
	if err := c.cache.Get(ctx, cacheKey, &cached); err == nil {
		log.Printf("[CachedRepository] Cache hit for default config: %s", category)
		return cached.ToSystemConfig(), nil
	}

	// 缓存未命中
	config, err := c.repo.GetDefaultByCategory(ctx, category)
	if err != nil {
		return nil, err
	}

	// 缓存结果
	cachedConfig := FromSystemConfig(config)
	if err := c.cache.Set(ctx, cacheKey, cachedConfig, c.ttl); err != nil {
		log.Printf("[CachedRepository] Failed to cache default config: %v", err)
	}

	return config, nil
}

// SetDefault 设置默认配置（清除缓存）
func (c *CachedRepository) SetDefault(ctx context.Context, id uint, category models.ConfigCategory) error {
	// 先清除该分类下所有配置的默认缓存
	c.InvalidateByCategory(ctx, category)

	return c.repo.SetDefault(ctx, id, category)
}

// Enable 启用配置（清除缓存）
func (c *CachedRepository) Enable(ctx context.Context, id uint) error {
	config, err := c.repo.GetByID(ctx, id)
	if err == nil {
		c.InvalidateByID(ctx, id)
		c.InvalidateByKey(ctx, config.Key)
		c.InvalidateByCategory(ctx, config.Category)
	}
	return c.repo.Enable(ctx, id)
}

// Disable 禁用配置（清除缓存）
func (c *CachedRepository) Disable(ctx context.Context, id uint) error {
	config, err := c.repo.GetByID(ctx, id)
	if err == nil {
		c.InvalidateByID(ctx, id)
		c.InvalidateByKey(ctx, config.Key)
		c.InvalidateByCategory(ctx, config.Category)
	}
	return c.repo.Disable(ctx, id)
}

// Count 统计配置数量
func (c *CachedRepository) Count(ctx context.Context) (int64, error) {
	return c.repo.Count(ctx)
}

// CountByCategory 按分类统计
func (c *CachedRepository) CountByCategory(ctx context.Context, category models.ConfigCategory) (int64, error) {
	return c.repo.CountByCategory(ctx, category)
}

// Exists 检查Key是否已存在
func (c *CachedRepository) Exists(ctx context.Context, key string) (bool, error) {
	return c.repo.Exists(ctx, key)
}

// Transaction 事务支持
func (c *CachedRepository) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return c.repo.Transaction(ctx, fn)
}

// EnsureKeyUnique 确保Key唯一
func (c *CachedRepository) EnsureKeyUnique(ctx context.Context, baseKey string) (string, error) {
	return c.repo.EnsureKeyUnique(ctx, baseKey)
}

// InvalidateByID 根据ID失效缓存
func (c *CachedRepository) InvalidateByID(ctx context.Context, id uint) {
	key := cacheKey("config:id:%d", id)
	if err := c.cache.Delete(ctx, key); err != nil {
		log.Printf("[CachedRepository] Failed to invalidate cache by ID: %v", err)
	}
}

// InvalidateByKey 根据Key失效缓存
func (c *CachedRepository) InvalidateByKey(ctx context.Context, key string) {
	cacheKey := cacheKey("config:key:%s", key)
	if err := c.cache.Delete(ctx, cacheKey); err != nil {
		log.Printf("[CachedRepository] Failed to invalidate cache by key: %v", err)
	}
}

// InvalidateByCategory 根据分类失效缓存
func (c *CachedRepository) InvalidateByCategory(ctx context.Context, category models.ConfigCategory) {
	// 失效默认配置缓存
	defaultKey := cacheKey("config:default:%s", category)
	if err := c.cache.Delete(ctx, defaultKey); err != nil {
		log.Printf("[CachedRepository] Failed to invalidate default cache: %v", err)
	}

	// 失效列表缓存
	listKey1 := cacheKey("config:list:%s:true", category)
	listKey2 := cacheKey("config:list:%s:false", category)
	c.cache.Delete(ctx, listKey1)
	c.cache.Delete(ctx, listKey2)

	// 失效所有列表缓存
	allListKey1 := cacheKey("config:list::true")
	allListKey2 := cacheKey("config:list::false")
	c.cache.Delete(ctx, allListKey1)
	c.cache.Delete(ctx, allListKey2)
}

// InvalidateAll 失效所有配置缓存
func (c *CachedRepository) InvalidateAll(ctx context.Context) {
	// 这里可以实现批量删除，如果缓存支持前缀删除
	// 目前只能手动清理已知的键模式
	log.Println("[CachedRepository] Manual cache invalidation requested")
}

// GetCacheStats 获取缓存统计信息（如果底层缓存支持）
func (c *CachedRepository) GetCacheStats() map[string]interface{} {
	return map[string]interface{}{
		"provider": c.cache.Name(),
		"ttl":      c.ttl.String(),
	}
}
