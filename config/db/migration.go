package config

import (
	"context"
	"log"

	"github.com/anoixa/image-bed/database/models"
)

// MigrateFromLegacy 从旧配置（config.yaml）迁移到数据库
func (m *Manager) MigrateFromLegacy(legacyStorage, legacyCache map[string]interface{}) error {
	ctx := context.Background()

	// 检查是否已有存储配置
	storageCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryStorage)
	if err != nil {
		return err
	}

	cacheCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryCache)
	if err != nil {
		return err
	}

	if storageCount == 0 && len(legacyStorage) > 0 {
		if err := m.migrateStorage(ctx, legacyStorage); err != nil {
			log.Printf("[ConfigMigration] Failed to migrate storage config: %v", err)
		}
	}

	if cacheCount == 0 && len(legacyCache) > 0 {
		if err := m.migrateCache(ctx, legacyCache); err != nil {
			log.Printf("[ConfigMigration] Failed to migrate cache config: %v", err)
		}
	}

	return nil
}

// MigrateJWTFromLegacy 从配置文件迁移 JWT 配置
func (m *Manager) MigrateJWTFromLegacy(legacySecret, legacyExpiresIn, legacyRefreshExpiresIn string) error {
	ctx := context.Background()

	// 检查是否已有 JWT 配置
	jwtCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		return err
	}
	if jwtCount > 0 || legacySecret == "" {
		return nil
	}

	return m.migrateJWT(ctx, legacySecret, legacyExpiresIn, legacyRefreshExpiresIn)
}

// migrateJWT 迁移 JWT 配置
func (m *Manager) migrateJWT(ctx context.Context, secret, expiresIn, refreshExpiresIn string) error {
	// 使用默认值
	if expiresIn == "" {
		expiresIn = "15m"
	}
	if refreshExpiresIn == "" {
		refreshExpiresIn = "168h"
	}

	configData := map[string]interface{}{
		"secret":            secret,
		"access_token_ttl":  expiresIn,
		"refresh_token_ttl": refreshExpiresIn,
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryJWT,
		Name:        "JWT Settings",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Migrated from config.yaml",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigMigration] JWT config migrated successfully")
	return nil
}

// migrateStorage 迁移存储配置
func (m *Manager) migrateStorage(ctx context.Context, legacy map[string]interface{}) error {
	storageType, _ := legacy["type"].(string)
	if storageType == "" {
		storageType = "local"
	}

	configData := map[string]interface{}{
		"type": storageType,
	}

	switch storageType {
	case "local":
		if local, ok := legacy["local"].(map[string]interface{}); ok {
			configData["local_path"] = getStringFromMap(local, "path", "./data/upload")
		} else {
			configData["local_path"] = "./data/upload"
		}
	case "minio":
		if minio, ok := legacy["minio"].(map[string]interface{}); ok {
			configData["endpoint"] = getStringFromMap(minio, "endpoint", "")
			configData["access_key_id"] = getStringFromMap(minio, "access_key_id", "")
			configData["secret_access_key"] = getStringFromMap(minio, "secret_access_key", "")
			configData["use_ssl"] = getBoolFromMap(minio, "use_ssl", false)
			configData["bucket_name"] = getStringFromMap(minio, "bucket_name", "")
		}
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryStorage,
		Name:        "Default Storage",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Migrated from config.yaml",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigMigration] Storage config migrated successfully")
	return nil
}

// migrateCache 迁移缓存配置
func (m *Manager) migrateCache(ctx context.Context, legacy map[string]interface{}) error {
	providerType, _ := legacy["provider"].(string)
	if providerType == "" {
		providerType = "memory"
	}

	configData := map[string]interface{}{
		"provider_type": providerType,
	}

	switch providerType {
	case "redis":
		if redis, ok := legacy["redis"].(map[string]interface{}); ok {
			configData["address"] = getStringFromMap(redis, "address", "localhost:6379")
			configData["password"] = getStringFromMap(redis, "password", "")
			configData["db"] = getIntFromMap(redis, "db", 0)
			configData["pool_size"] = getIntFromMap(redis, "pool_size", 10)
			configData["min_idle_conns"] = getIntFromMap(redis, "min_idle_conns", 5)
		} else {
			configData["address"] = "localhost:6379"
		}
	case "memory":
		if memory, ok := legacy["memory"].(map[string]interface{}); ok {
			configData["num_counters"] = getInt64FromMap(memory, "num_counters", 1000000)
			configData["max_cost"] = getInt64FromMap(memory, "max_cost", 1073741824)
			configData["buffer_items"] = getInt64FromMap(memory, "buffer_items", 64)
			configData["metrics"] = getBoolFromMap(memory, "metrics", true)
		} else {
			configData["num_counters"] = int64(1000000)
			configData["max_cost"] = int64(1073741824)
		}
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryCache,
		Name:        "Default Cache",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Migrated from config.yaml",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigMigration] Cache config migrated successfully")
	return nil
}

// CreateDefaultConfigs 创建默认配置（新部署使用）
func (m *Manager) CreateDefaultConfigs() error {
	ctx := context.Background()

	// 检查是否已有存储配置
	storageCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryStorage)
	if err != nil {
		return err
	}

	// 检查是否已有缓存配置
	cacheCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryCache)
	if err != nil {
		return err
	}

	// 检查是否已有 JWT 配置
	jwtCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryJWT)
	if err != nil {
		return err
	}

	// 创建默认配置
	if storageCount == 0 {
		if err := m.createDefaultStorage(ctx); err != nil {
			log.Printf("[ConfigMigration] Failed to create default storage config: %v", err)
		}
	}
	if cacheCount == 0 {
		if err := m.createDefaultCache(ctx); err != nil {
			log.Printf("[ConfigMigration] Failed to create default cache config: %v", err)
		}
	}
	if jwtCount == 0 {
		if err := m.EnsureDefaultJWTConfig(ctx); err != nil {
			log.Printf("[ConfigMigration] Failed to create default JWT config: %v", err)
		}
	}

	return nil
}

// createDefaultStorage 创建默认存储配置（本地存储）
func (m *Manager) createDefaultStorage(ctx context.Context) error {
	configData := map[string]interface{}{
		"type":       "local",
		"local_path": "./data/upload",
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryStorage,
		Name:        "Local Storage",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Default local file storage",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigMigration] Default storage config created")
	return nil
}

// createDefaultCache 创建默认缓存配置（内存缓存）
func (m *Manager) createDefaultCache(ctx context.Context) error {
	configData := map[string]interface{}{
		"provider_type": "memory",
		"num_counters":  int64(1000000),
		"max_cost":      int64(1073741824), // 1GB
		"buffer_items":  int64(64),
		"metrics":       true,
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryCache,
		Name:        "Memory Cache",
		Config:      configData,
		IsEnabled:   BoolPtr(true),
		IsDefault:   BoolPtr(true),
		Description: "Default in-memory cache",
	}

	_, err := m.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Println("[ConfigMigration] Default cache config created")
	return nil
}

// 辅助函数
func getBoolFromMap(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getIntFromMap(m map[string]interface{}, key string, defaultVal int) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return defaultVal
	}
}

func getInt64FromMap(m map[string]interface{}, key string, defaultVal int64) int64 {
	switch v := m[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return defaultVal
	}
}
