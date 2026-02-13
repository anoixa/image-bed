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
	Host         string        `mapstructure:"host"`  // 服务器主机，如 "localhost" 或 "0.0.0.0"
	Port         int           `mapstructure:"port"`  // 服务器端口，如 8080
	Domain       string        `mapstructure:"domain"` // 外部访问域名，用于生成 URL
	ReadTimeout  time.Duration `yaml:"readTimeout"`
	WriteTimeout time.Duration `yaml:"writeTimeout"`
	IdleTimeout  time.Duration `yaml:"idleTimeout"`

	Jwt            Jwt            `mapstructure:"jwt"`
	DatabaseConfig DatabaseConfig `mapstructure:"database"`
	StorageConfig  StorageConfig  `mapstructure:"storage"`
	CacheConfig    CacheConfig    `mapstructure:"cache"`
}

// Addr 返回监听地址，格式为 "host:port"
func (s ServerConfig) Addr() string {
	if s.Host == "" {
		s.Host = "0.0.0.0"
	}
	if s.Port == 0 {
		s.Port = 8080
	}
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// BaseURL 返回基础 URL，用于生成图片链接
func (s ServerConfig) BaseURL() string {
	if s.Domain != "" {
		return s.Domain
	}
	// 默认使用 localhost
	host := s.Host
	if host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d", host, s.Port)
}

type DatabaseConfig struct {
	Type     string `mapstructure:"type"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`

	DatabaseFilePath string `mapstructure:"database_file_path"` //sqlite数据库文件路径

	MaxOpenConns    int `mapstructure:"max_open_conns"`
	MaxIdleConns    int `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int `mapstructure:"conn_max_lifetime"`
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
}

type LocalStorageConfig struct {
	Path string `mapstructure:"path"` // 本地文件存储路径
}

type CacheConfig struct {
	Provider           string       `mapstructure:"provider"`
	Redis              RedisConfig  `mapstructure:"redis"`
	Memory             MemoryConfig `mapstructure:"memory"`
	MaxImageCacheSize  int64        `mapstructure:"max_image_cache_size_mb"` // 最大图片缓存大小（MB），0表示无限制
	EnableImageCaching bool         `mapstructure:"enable_image_caching"`    // 是否启用图片缓存

	ImageCacheTTL     int `mapstructure:"image_cache_ttl"`      // 图片元数据缓存时间（秒）
	ImageDataCacheTTL int `mapstructure:"image_data_cache_ttl"` // 图片数据缓存时间（秒）
}

type RedisConfig struct {
	Address  string `mapstructure:"address"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`

	// 连接池配置
	PoolSize           int    `mapstructure:"pool_size"`
	MinIdleConns       int    `mapstructure:"min_idle_conns"`
	MaxConnAge         string `mapstructure:"max_conn_age"`
	PoolTimeout        string `mapstructure:"pool_timeout"`
	IdleTimeout        string `mapstructure:"idle_timeout"`
	IdleCheckFrequency string `mapstructure:"idle_check_frequency"`
}

type MemoryConfig struct {
	NumCounters int64 `mapstructure:"num_counters"`
	MaxCost     int64 `mapstructure:"max_cost"`
	BufferItems int64 `mapstructure:"buffer_items"`
	Metrics     bool  `mapstructure:"metrics"`
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
	viper.SetDefault("server.ReadTimeout", 15*time.Second)
	viper.SetDefault("server.WriteTimeout", 30*time.Second)
	viper.SetDefault("server.IdleTimeout", 120*time.Second)

	viper.SetDefault("server.storage.type", "local")
	viper.SetDefault("server.storage.local.path", "data/upload")

	// Database connection pool defaults
	viper.SetDefault("server.database.max_open_conns", 100)
	viper.SetDefault("server.database.max_idle_conns", 10)
	viper.SetDefault("server.database.conn_max_lifetime", 3600)
	viper.SetDefault("server.storage.minio.max_idle_conns", 256)
	viper.SetDefault("server.storage.minio.max_idle_conns_per_host", 16)
	viper.SetDefault("server.storage.minio.idle_conn_timeout", "60s")
	viper.SetDefault("server.storage.minio.tls_handshake_timeout", "10s")
	viper.SetDefault("server.cache.provider", "memory")

	// Memory cache defaults
	viper.SetDefault("server.cache.memory.num_counters", 1000000)
	viper.SetDefault("server.cache.memory.max_cost", 1073741824) // 1GB
	viper.SetDefault("server.cache.memory.buffer_items", 64)
	viper.SetDefault("server.cache.memory.metrics", true)
	viper.SetDefault("server.cache.max_image_cache_size_mb", 10) // 最大缓存 10MB 的图片
	viper.SetDefault("server.cache.enable_image_caching", false) // 默认不启用图片缓存

	// Redis
	viper.SetDefault("server.cache.redis.pool_size", 10)
	viper.SetDefault("server.cache.redis.min_idle_conns", 5)
	viper.SetDefault("server.cache.redis.max_conn_age", "30m")
	viper.SetDefault("server.cache.redis.pool_timeout", "30s")
	viper.SetDefault("server.cache.redis.idle_timeout", "10m")
	viper.SetDefault("server.cache.redis.idle_check_frequency", "1m")

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
