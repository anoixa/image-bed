package config

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var (
	globalConfig Config
	once         sync.Once
)

// Config 扁平化配置结构体
type Config struct {
	// 服务器配置
	ServerHost         string        `mapstructure:"server_host"`
	ServerPort         int           `mapstructure:"server_port"`
	ServerDomain       string        `mapstructure:"server_domain"`
	ServerReadTimeout  time.Duration `mapstructure:"server_read_timeout"`
	ServerWriteTimeout time.Duration `mapstructure:"server_write_timeout"`
	ServerIdleTimeout  time.Duration `mapstructure:"server_idle_timeout"`

	// 数据库配置
	DBType            string `mapstructure:"db_type"`
	DBHost            string `mapstructure:"db_host"`
	DBPort            int    `mapstructure:"db_port"`
	DBUsername        string `mapstructure:"db_username"`
	DBPassword        string `mapstructure:"db_password"`
	DBName            string `mapstructure:"db_name"`
	DBFilePath        string `mapstructure:"db_file_path"`
	DBMaxOpenConns    int    `mapstructure:"db_max_open_conns"`
	DBMaxIdleConns    int    `mapstructure:"db_max_idle_conns"`
	DBConnMaxLifetime int    `mapstructure:"db_conn_max_lifetime"`

	// 缓存配置
	CacheMaxImageCacheSizeMB int64 `mapstructure:"cache_max_image_cache_size_mb"`
	CacheEnableImageCaching  bool  `mapstructure:"cache_enable_image_caching"`
	CacheImageCacheTTL       int   `mapstructure:"cache_image_cache_ttl"`
	CacheImageDataCacheTTL   int   `mapstructure:"cache_image_data_cache_ttl"`

	// 缓存提供者配置
	CacheType          string `mapstructure:"cache_type"`
	CacheRedisAddr     string `mapstructure:"cache_redis_addr"`
	CacheRedisPassword string `mapstructure:"cache_redis_password"`
	CacheRedisDB       int    `mapstructure:"cache_redis_db"`

	// 限流配置
	RateLimitApiRPS     float64       `mapstructure:"rate_limit_api_rps"`
	RateLimitApiBurst   int           `mapstructure:"rate_limit_api_burst"`
	RateLimitImageRPS   float64       `mapstructure:"rate_limit_image_rps"`
	RateLimitImageBurst int           `mapstructure:"rate_limit_image_burst"`
	RateLimitAuthRPS    float64       `mapstructure:"rate_limit_auth_rps"`
	RateLimitAuthBurst  int           `mapstructure:"rate_limit_auth_burst"`
	RateLimitExpireTime time.Duration `mapstructure:"rate_limit_expire_time"`

	// 上传配置
	UploadMaxSizeMB       int `mapstructure:"upload_max_size_mb"`
	UploadMaxBatchTotalMB int `mapstructure:"upload_max_batch_total_mb"`

	// Worker 配置
	WorkerCount int `mapstructure:"worker_count"`
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
	setDefaults()

	viper.SetConfigFile(".env")
	viper.SetConfigType("env")

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "Info: .env file not found, using defaults and environment variables")
	} else {
		fmt.Fprintln(os.Stderr, "Info: Loaded configuration from .env file")
	}

	viper.AutomaticEnv()
	for _, key := range viper.AllKeys() {
		viper.BindEnv(key)
	}

	if err := viper.Unmarshal(&globalConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: Unable to unmarshal config, %v\n", err)
		os.Exit(1)
	}

	// WorkerCount: -1 = 使用 CPU 线程数, 0 = 使用默认值 (max(2, CPU核心数)), >0 = 使用指定值
	switch {
	case globalConfig.WorkerCount < 0:
		// 使用当前 CPU 线程数
		globalConfig.WorkerCount = runtime.GOMAXPROCS(0)
	case globalConfig.WorkerCount == 0:
		// 使用默认值
		globalConfig.WorkerCount = getCpus()
	// default: 使用配置文件中指定的值
	}
}

// setDefaults 设置默认值
func setDefaults() {
	// 服务器配置默认值
	viper.SetDefault("server_host", "127.0.0.1")
	viper.SetDefault("server_port", 8080)
	viper.SetDefault("server_domain", "")
	viper.SetDefault("server_read_timeout", "15s")
	viper.SetDefault("server_write_timeout", "30s")
	viper.SetDefault("server_idle_timeout", "120s")

	// 数据库配置默认值
	viper.SetDefault("db_type", "sqlite")
	viper.SetDefault("db_host", "localhost")
	viper.SetDefault("db_port", 5432)
	viper.SetDefault("db_username", "postgres")
	viper.SetDefault("db_password", "")
	viper.SetDefault("db_name", "image-bed")
	viper.SetDefault("db_file_path", "")
	viper.SetDefault("db_max_open_conns", 100)
	viper.SetDefault("db_max_idle_conns", 25)
	viper.SetDefault("db_conn_max_lifetime", 3600)

	// 缓存配置默认值
	viper.SetDefault("cache_max_image_cache_size_mb", 10)
	viper.SetDefault("cache_enable_image_caching", false)
	viper.SetDefault("cache_image_cache_ttl", 3600)
	viper.SetDefault("cache_image_data_cache_ttl", 3600)

	// 缓存提供者配置默认值
	viper.SetDefault("cache_type", "memory")
	viper.SetDefault("cache_redis_addr", "localhost:6379")
	viper.SetDefault("cache_redis_password", "")
	viper.SetDefault("cache_redis_db", 0)

	// 限流配置默认值
	viper.SetDefault("rate_limit_api_rps", 30.0)
	viper.SetDefault("rate_limit_api_burst", 60)
	viper.SetDefault("rate_limit_image_rps", 100.0)
	viper.SetDefault("rate_limit_image_burst", 200)
	viper.SetDefault("rate_limit_auth_rps", 0.5)
	viper.SetDefault("rate_limit_auth_burst", 5)
	viper.SetDefault("rate_limit_expire_time", "10m")

	// 上传配置默认值
	viper.SetDefault("upload_max_size_mb", 50)
	viper.SetDefault("upload_max_batch_total_mb", 500)

	// Worker 配置默认值
	viper.SetDefault("worker_count", 0) // 0 表示使用默认值
}

// Addr 返回监听地址，格式为 "host:port"
func (c *Config) Addr() string {
	host := c.ServerHost
	if host == "" {
		host = "0.0.0.0"
	}
	port := c.ServerPort
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// BaseURL 返回基础 URL，用于生成图片链接
func (c *Config) BaseURL() string {
	if c.ServerDomain != "" {
		return c.ServerDomain
	}
	// 默认使用 localhost
	host := c.ServerHost
	if host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d", host, c.ServerPort)
}

// GetWorkerCount 返回 worker 数量
func (c *Config) GetWorkerCount() int {
	if c.WorkerCount <= 0 {
		return getCpus()
	}
	return c.WorkerCount
}

// getCpus 获取默认线程数量
func getCpus() int {
	n := runtime.GOMAXPROCS(0)
	if n < 2 {
		return 2
	}
	return n
}
