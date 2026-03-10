package config

import (
	"sync"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
)

// CacheLayer 配置缓存层
type CacheLayer struct {
	localCache map[string]interface{}
	mutex      sync.RWMutex
}

// NewCacheLayer 创建缓存层
func NewCacheLayer() *CacheLayer {
	return &CacheLayer{
		localCache: make(map[string]interface{}),
	}
}

// Invalidate 使指定类别的缓存失效
func (c *CacheLayer) Invalidate(category models.ConfigCategory) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	switch category {
	case models.ConfigCategoryJWT:
		delete(c.localCache, keyJWT)
	case models.ConfigCategoryStorage:
		delete(c.localCache, keyStorage)
	case models.ConfigCategoryImageProcessing:
		delete(c.localCache, keyImageProcessing)
	}
}

// GetJWT 获取缓存的 JWT 配置
func (c *CacheLayer) GetJWT() *JWTConfig {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if val, ok := c.localCache[keyJWT]; ok {
		return val.(*JWTConfig)
	}
	return nil
}

// SetJWT 设置 JWT 配置缓存
func (c *CacheLayer) SetJWT(cfg *JWTConfig) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache[keyJWT] = cfg
}

// GetStorage 获取缓存的存储配置
func (c *CacheLayer) GetStorage() []storage.StorageConfig {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if val, ok := c.localCache[keyStorage]; ok {
		return val.([]storage.StorageConfig)
	}
	return nil
}

// SetStorage 设置存储配置缓存
func (c *CacheLayer) SetStorage(cfg []storage.StorageConfig) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache[keyStorage] = cfg
}

// GetImageProcessing 获取图片处理配置
func (c *CacheLayer) GetImageProcessing() *ImageProcessingSettings {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if val, ok := c.localCache[keyImageProcessing]; ok {
		return val.(*ImageProcessingSettings)
	}
	return nil
}

// SetImageProcessing 设置图片处理配置缓存
func (c *CacheLayer) SetImageProcessing(settings *ImageProcessingSettings) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache[keyImageProcessing] = settings
}

const (
	keyJWT             = "config:jwt"
	keyStorage         = "config:storage"
	keyImageProcessing = "config:image_processing"
)
