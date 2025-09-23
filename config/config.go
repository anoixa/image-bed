package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var (
	globalConfig Config
	once         sync.Once
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
}

type ServerConfig struct {
	Addr           string         `mapstructure:"addr"`
	Domain         string         `mapstructure:"domain"`
	BaseURL        string         `mapstructure:"base_url"`
	Jwt            Jwt            `mapstructure:"jwt"`
	DatabaseConfig DatabaseConfig `mapstructure:"database"`
	StorageConfig  StorageConfig  `mapstructure:"storage"`
	CacheConfig    CacheConfig    `mapstructure:"cache"`
}

type DatabaseConfig struct {
	Type     string `mapstructure:"type"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`

	DatabaseFilePath string `mapstructure:"database_file_path"` //sqlite数据库文件路径
}

type StorageConfig struct {
	Type  string             `mapstructure:"type"`
	Minio MinioConfig        `mapstructure:"minio"`
	Local LocalStorageConfig `mapstructure:"local"`
}

type Jwt struct {
	Secret           string `mapstructure:"secret"`
	ExpiresIn        string `mapstructure:"expires_in"`
	RefreshExpiresIn string `mapstructure:"refresh_expires_in"`
}

type MinioConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	BucketName      string `mapstructure:"bucket_name"`

	// 连接池配置
	MaxIdleConns        int    `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost int    `mapstructure:"max_idle_conns_per_host"`
	IdleConnTimeout     string `mapstructure:"idle_conn_timeout"`
	TLSHandshakeTimeout string `mapstructure:"tls_handshake_timeout"`
}

type LocalStorageConfig struct {
	Path string `mapstructure:"path"` // 本地文件存储路径
}

type CacheConfig struct {
	Provider string        `mapstructure:"provider"`
	Redis    RedisConfig   `mapstructure:"redis"`
	Memory   GoCacheConfig `mapstructure:"memory"`
}

type RedisConfig struct {
	Address  string `mapstructure:"address"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type GoCacheConfig struct {
	DefaultExpiration time.Duration `mapstructure:"default_expiration"`
	CleanupInterval   time.Duration `mapstructure:"cleanup_interval"`
}

// InitConfig Initialize configuration
func InitConfig() {
	once.Do(func() {
		loadConfig()
	})
}

func Get() *Config {
	return &globalConfig
}

// loadConfig Core configuration loading
func loadConfig() {
	// 默认值
	viper.SetDefault("server.host", "127.0.0.1")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.storage.type", "local")
	viper.SetDefault("server.storage.local.path", "data/upload")
	viper.SetDefault("server.storage.minio.max_idle_conns", 256)
	viper.SetDefault("server.storage.minio.max_idle_conns_per_host", 16)
	viper.SetDefault("server.storage.minio.idle_conn_timeout", "60s")
	viper.SetDefault("server.storage.minio.tls_handshake_timeout", "10s")
	viper.SetDefault("server.cache.provider", "memory")
	// GoCache专属默认值
	viper.SetDefault("server.cache.memory.default_expiration", "30m")
	viper.SetDefault("server.cache.memory.cleanup_interval", "10m")

	configFileFromFlag := viper.GetString("config_file_path")

	// 优先从 flag 读取配置文件路径
	if configFileFromFlag != "" {
		fmt.Fprintf(os.Stderr, "Attempting to use config file: %s\n", configFileFromFlag)
		viper.SetConfigFile(configFileFromFlag)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	// 读取环境变量
	viper.SetEnvPrefix("IMAGE_BED")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError

		if errors.As(err, &configFileNotFoundError) {
			if configFileFromFlag != "" {
				fmt.Fprintf(os.Stderr, "Error: Configuration file not found at specified path: %s\n", configFileFromFlag)
				os.Exit(1)
			} else {
				fmt.Fprintln(os.Stderr, "Warning: Configuration file not found. Using defaults and environment variables.")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error reading configuration file: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Using configuration file:", viper.ConfigFileUsed())
	}

	if err := viper.Unmarshal(&globalConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: Unable to unmarshal config into struct, %v\n", err)
		os.Exit(1)
	}
}
