package utils

import (
	"os"
	"path/filepath"
)

// GetExecutableDir 获取可执行文件所在目录
// 用于确保数据文件存储在可执行文件旁边，而不是当前工作目录
func GetExecutableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

// GetDataDir 获取数据目录路径（可执行文件所在目录下的 data 文件夹）
func GetDataDir() string {
	return filepath.Join(GetExecutableDir(), "data")
}
