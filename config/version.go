package config

var (
	Version    string = "dev"
	CommitHash string = ""
)

// IsProduction 判断是否为生产环境
// 生产环境：Version 为 "release" 且 CommitHash 不为空
// 开发环境：Version 为 "dev"（CommitHash 可为空或不为空）
func IsProduction() bool {
	return Version == "release" && CommitHash != ""
}

// IsDevelopment 判断是否为开发环境
func IsDevelopment() bool {
	return Version == "dev"
}
