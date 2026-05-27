package config

import (
	"os"
	"strings"
)

var (
	Version    string = "dev"
	CommitHash string = ""
)

// IsProduction 判断是否为生产环境
// 优先使用 APP_ENV/IMAGE_BED_ENV；未显式配置时回退到构建版本信息。
func IsProduction() bool {
	if env := runtimeEnvironment(); env != "" {
		return env == "production" || env == "prod"
	}
	return Version == "release" && CommitHash != ""
}

// IsDevelopment 判断是否为开发环境
func IsDevelopment() bool {
	if env := runtimeEnvironment(); env != "" {
		return env == "development" || env == "dev"
	}
	return Version == "dev"
}

func runtimeEnvironment() string {
	env := strings.TrimSpace(os.Getenv("APP_ENV"))
	if env == "" {
		env = strings.TrimSpace(os.Getenv("IMAGE_BED_ENV"))
	}
	return strings.ToLower(env)
}
