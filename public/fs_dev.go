//go:build dev
// +build dev

package public

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// DistFS 开发模式下使用本地文件系统
var DistFS = func() http.FileSystem {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	distPath := filepath.Join(dir, "dist")
	return http.Dir(distPath)
}()

// FileSystem 开发模式下返回 nil
func FileSystem() any {
	return nil
}

// ReadFile 从磁盘读取文件
func ReadFile(name string) ([]byte, error) {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return os.ReadFile(filepath.Join(dir, "dist", name))
}

// Exists 检查文件是否存在
func Exists(name string) bool {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	_, err := os.Stat(filepath.Join(dir, "dist", name))
	return err == nil
}

// ListFiles 列出开发目录下的文件
func ListFiles() ([]string, error) {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	distPath := filepath.Join(dir, "dist")

	var files []string
	err := filepath.WalkDir(distPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(distPath, p)
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}
