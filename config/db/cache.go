package config

import (
	"sync"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
)

// CacheLayer 配置缓存层
type CacheLayer struct {
	localCache map[string]any
	mutex      sync.RWMutex
}

// NewCacheLayer 创建缓存层
func NewCacheLayer() *CacheLayer {
	return &CacheLayer{
		localCache: make(map[string]any),
	}
}

// Invalidate 使指定类别的缓存失效
func (c *CacheLayer) Invalidate(category models.ConfigCategory) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	switch category {
	case models.ConfigCategoryStorage:
		delete(c.localCache, keyStorage)
	case models.ConfigCategoryImageProcessing:
		delete(c.localCache, keyImageProcessing)
	case models.ConfigCategorySystem:
		delete(c.localCache, keyTransferMode)
		delete(c.localCache, keyAutoDirectThresholdBytes)
	}
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

func (c *CacheLayer) GetTransferMode() (storage.TransferMode, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if val, ok := c.localCache[keyTransferMode]; ok {
		return val.(storage.TransferMode), true
	}
	return "", false
}

func (c *CacheLayer) SetTransferMode(mode storage.TransferMode) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache[keyTransferMode] = mode
}

func (c *CacheLayer) GetAutoDirectThresholdBytes() (int64, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if val, ok := c.localCache[keyAutoDirectThresholdBytes]; ok {
		return val.(int64), true
	}
	return 0, false
}

func (c *CacheLayer) SetAutoDirectThresholdBytes(thresholdBytes int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache[keyAutoDirectThresholdBytes] = thresholdBytes
}

const (
	keyStorage                  = "config:storage"
	keyImageProcessing          = "config:image_processing"
	keyTransferMode             = "config:transfer_mode"
	keyAutoDirectThresholdBytes = "config:auto_direct_threshold_bytes"
)

// InvalidateAll 清除所有缓存
func (c *CacheLayer) InvalidateAll() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.localCache = make(map[string]any)
}
