//go:build !dev
// +build !dev

package public

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed all:dist
var embeddedFS embed.FS

// distFS 是 dist 子目录的文件系统
var distFS, _ = fs.Sub(embeddedFS, "dist")

// DistFS 嵌入的文件系统（指向 dist 子目录）
var DistFS = http.FS(distFS)

// FileSystem 返回原始 embed.FS
func FileSystem() embed.FS {
	return embeddedFS
}

// ReadFile 读取嵌入的文件内容
func ReadFile(name string) ([]byte, error) {
	return embeddedFS.ReadFile(path.Join("dist", name))
}

// Exists 检查文件是否存在
func Exists(name string) bool {
	_, err := distFS.Open(name)
	return err == nil
}

// ListFiles 列出 dist 目录下的所有文件
func ListFiles() ([]string, error) {
	return listFilesRecursive("dist")
}

func listFilesRecursive(root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(embeddedFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
