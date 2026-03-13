package public

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler 返回处理静态文件的 Gin HandlerFunc
func Handler() gin.HandlerFunc {
	fileServer := http.FileServer(DistFS)

	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path

		// 跳过 API 路径，让后续路由处理
		if isAPIPath(requestPath) {
			c.Next()
			return
		}

		// 清理路径
		cleanPath := path.Clean(requestPath)
		if cleanPath == "." {
			cleanPath = "/"
		}

		fileExists := checkFileExists(cleanPath)

		if !fileExists {
			// 尝试作为目录查找 index.html
			if !strings.HasSuffix(cleanPath, "/") {
				fileExists = checkFileExists(cleanPath + "/index.html")
			}
		}

		if !fileExists {
			// SPA 回退：所有未知路径都返回 index.html
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		// 文件存在，正常服务
		fileServer.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

// isAPIPath 检查路径是否为 API 路径
func isAPIPath(p string) bool {
	apiPaths := []string{
		"/api/",
		"/images/",
		"/thumbnails/",
		"/health",
		"/version",
		"/metrics",
		"/swagger/",
	}
	for _, prefix := range apiPaths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// checkFileExists 检查文件是否存在于 dist 目录
func checkFileExists(requestPath string) bool {
	cleanPath := path.Clean(requestPath)
	if cleanPath == "." || cleanPath == "/" {
		return Exists("index.html")
	}
	// 移除开头的 /
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	return Exists(cleanPath)
}

// StaticFileServer 返回标准库的 http.Handler
func StaticFileServer() http.Handler {
	return http.FileServer(DistFS)
}

// PrintFiles 打印嵌入的所有文件
func PrintFiles() {
	files, err := ListFiles()
	if err != nil {
		return
	}
	for _, f := range files {
		println("  -", f)
	}
}

// IsDevMode 检查当前是否为开发模式
func IsDevMode() bool {
	_, isDir := DistFS.(interface {
		Open(name string) (http.File, error)
	})

	_, err := os.Stat("public/dist")
	return isDir && err == nil
}
