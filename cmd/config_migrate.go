package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/anoixa/image-bed/database/models"
	configSvc "github.com/anoixa/image-bed/config/db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// configMigrateCmd 将 config.yaml 中的 storage/cache 配置迁移到数据库
var configMigrateCmd = &cobra.Command{
	Use:   "config-migrate",
	Short: "Migrate storage/cache config from config.yaml to database",
	Long: `Migrate storage and cache configurations from config.yaml to the database.
This command reads the old configuration format and creates corresponding entries in the system_configs table.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runConfigMigration(); err != nil {
			log.Fatalf("Config migration failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(configMigrateCmd)
}

func runConfigMigration() error {
	// 加载配置
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config.yaml: %w", err)
	}

	// 连接到数据库
	db, err := connectDatabaseFromViper()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 确保表存在
	if err := db.AutoMigrate(&models.SystemConfig{}); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// 创建配置管理器
	manager := configSvc.NewManager(db, "./data")
	if err := manager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}

	ctx := context.Background()

	// 检查是否已有配置
	count, err := manager.GetRepo().Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to check existing configs: %w", err)
	}

	if count > 1 { // 大于1因为 canary 也算一个
		log.Println("Database already has configuration entries. Skipping migration.")
		return nil
	}

	// 迁移 storage 配置
	if err := migrateStorageConfig(ctx, manager); err != nil {
		return fmt.Errorf("failed to migrate storage config: %w", err)
	}

	// 迁移 cache 配置
	if err := migrateCacheConfig(ctx, manager); err != nil {
		return fmt.Errorf("failed to migrate cache config: %w", err)
	}

	log.Println("Configuration migration completed successfully!")
	log.Printf("You can now remove the 'storage' and 'cache' sections from your config.yaml")
	return nil
}

func connectDatabaseFromViper() (*gorm.DB, error) {
	dbType := viper.GetString("server.database.type")
	host := viper.GetString("server.database.host")
	port := viper.GetInt("server.database.port")
	username := viper.GetString("server.database.username")
	password := viper.GetString("server.database.password")
	database := viper.GetString("server.database.database")
	dbFilePath := viper.GetString("server.database.database_file_path")

	switch dbType {
	case "postgresql":
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
			host, port, username, password, database)
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	case "sqlite":
		return gorm.Open(sqlite.Open(dbFilePath), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

func migrateStorageConfig(ctx context.Context, manager *configSvc.Manager) error {
	storageType := viper.GetString("server.storage.type")
	if storageType == "" {
		log.Println("No storage config found in config.yaml, skipping...")
		return nil
	}

	log.Printf("Migrating storage config (type: %s)...", storageType)

	var configData map[string]interface{}
	var name string

	switch storageType {
	case "minio":
		configData = map[string]interface{}{
			"type":              "minio",
			"endpoint":          viper.GetString("server.storage.minio.endpoint"),
			"access_key_id":     viper.GetString("server.storage.minio.access_key_id"),
			"secret_access_key": viper.GetString("server.storage.minio.secret_access_key"),
			"use_ssl":           viper.GetBool("server.storage.minio.use_ssl"),
			"bucket_name":       viper.GetString("server.storage.minio.bucket_name"),
		}
		name = "MinIO Storage"
	case "local":
		configData = map[string]interface{}{
			"type":       "local",
			"local_path": viper.GetString("server.storage.local.path"),
		}
		name = "Local Storage"
	default:
		log.Printf("Unknown storage type: %s, skipping...", storageType)
		return nil
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryStorage,
		Name:        name,
		Config:      configData,
		IsEnabled:   boolPtr(true),
		IsDefault:   boolPtr(true),
		Description: "Migrated from config.yaml",
	}

	resp, err := manager.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Printf("Storage config migrated successfully (ID: %d)", resp.ID)
	return nil
}

func migrateCacheConfig(ctx context.Context, manager *configSvc.Manager) error {
	provider := viper.GetString("server.cache.provider")
	if provider == "" {
		log.Println("No cache config found in config.yaml, skipping...")
		return nil
	}

	log.Printf("Migrating cache config (provider: %s)...", provider)

	var configData map[string]interface{}
	var name string

	switch provider {
	case "redis":
		configData = map[string]interface{}{
			"provider_type":         "redis",
			"address":               viper.GetString("server.cache.redis.address"),
			"password":              viper.GetString("server.cache.redis.password"),
			"db":                    viper.GetInt("server.cache.redis.db"),
			"pool_size":             viper.GetInt("server.cache.redis.pool_size"),
			"min_idle_conns":        viper.GetInt("server.cache.redis.min_idle_conns"),
		}
		name = "Redis Cache"
	case "memory":
		configData = map[string]interface{}{
			"provider_type": "memory",
			"num_counters":  viper.GetInt64("server.cache.memory.num_counters"),
			"max_cost":      viper.GetInt64("server.cache.memory.max_cost"),
			"buffer_items":  viper.GetInt64("server.cache.memory.buffer_items"),
			"metrics":       viper.GetBool("server.cache.memory.metrics"),
		}
		name = "Memory Cache"
	default:
		log.Printf("Unknown cache provider: %s, using default memory cache", provider)
		configData = map[string]interface{}{
			"provider_type": "memory",
			"num_counters":  int64(1000000),
			"max_cost":      int64(1073741824),
			"buffer_items":  int64(64),
			"metrics":       true,
		}
		name = "Memory Cache"
	}

	req := &models.SystemConfigStoreRequest{
		Category:    models.ConfigCategoryCache,
		Name:        name,
		Config:      configData,
		IsEnabled:   boolPtr(true),
		IsDefault:   boolPtr(true),
		Description: "Migrated from config.yaml",
	}

	resp, err := manager.CreateConfig(ctx, req, 0)
	if err != nil {
		return err
	}

	log.Printf("Cache config migrated successfully (ID: %d)", resp.ID)
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}
