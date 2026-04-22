package config

import (
	"context"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
)

var configMigrationLog = utils.ForModule("ConfigMigration")

// MigrateFromLegacy 从旧配置（config.yaml）迁移到数据库
func (m *Manager) MigrateFromLegacy(legacyStorage map[string]any) error {
	ctx := context.Background()

	storageCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryStorage)
	if err != nil {
		return err
	}

	// 只迁移 storage 配置（cache 配置现在从环境变量读取）
	if storageCount == 0 && len(legacyStorage) > 0 {
		if err := m.migrateStorage(ctx, legacyStorage); err != nil {
			configMigrationLog.Errorf("Failed to migrate storage config: %v", err)
		}
	}

	return nil
}

// migrateStorage 迁移存储配置
func (m *Manager) migrateStorage(ctx context.Context, legacy map[string]any) error {
	storageType, _ := legacy["type"].(string)
	if storageType == "" {
		storageType = "local"
	}

	configData := map[string]any{
		"type": storageType,
	}

	switch storageType {
	case "local":
		if local, ok := legacy["local"].(map[string]any); ok {
			configData["local_path"] = getStringFromMap(local, "path", "./data/upload")
		} else {
			configData["local_path"] = "./data/upload"
		}
	case "minio":
		if minio, ok := legacy["minio"].(map[string]any); ok {
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

	configMigrationLog.Infof("Storage config migrated successfully")
	return nil
}

// CreateDefaultConfigs 创建默认配置（新部署使用）
func (m *Manager) CreateDefaultConfigs() error {
	ctx := context.Background()

	storageCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryStorage)
	if err != nil {
		return err
	}

	// 检查是否已有图片处理配置
	imageProcessingCount, err := m.repo.CountByCategory(ctx, models.ConfigCategoryImageProcessing)
	if err != nil {
		return err
	}

	if storageCount == 0 {
		if err := m.createDefaultStorage(ctx); err != nil {
			configMigrationLog.Errorf("Failed to create default storage config: %v", err)
		}
	}

	if imageProcessingCount == 0 {
		if err := m.ensureDefaultImageProcessingConfig(ctx); err != nil {
			configMigrationLog.Errorf("Failed to create default image processing config: %v", err)
		}
	}

	return nil
}

// createDefaultStorage 创建默认存储配置（本地存储）
func (m *Manager) createDefaultStorage(ctx context.Context) error {
	configData := map[string]any{
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

	configMigrationLog.Infof("Default storage config created")
	return nil
}

// 辅助函数
func getBoolFromMap(m map[string]any, key string, defaultVal bool) bool {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true" || val == "1" || val == "yes" || val == "on"
		case int:
			return val != 0
		case int64:
			return val != 0
		case float64:
			return val != 0
		}
	}
	return defaultVal
}
