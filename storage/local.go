package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// localStorage 本地文件存储。
type localStorage struct {
	absBasePath string
}

// newLocalStorage 创建储存路径
func newLocalStorage(basePath string) (*localStorage, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for '%s': %w", basePath, err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local storage directory '%s': %w", absPath, err)
	}

	return &localStorage{
		absBasePath: absPath + string(os.PathSeparator),
	}, nil
}

// Save 保存文件
func (s *localStorage) Save(identifier string, file io.Reader) error {
	if !isValidIdentifier(identifier) {
		return fmt.Errorf("invalid file identifier: %s", identifier)
	}

	dstPath := filepath.Join(s.absBasePath, identifier)

	// 确保最终路径在 basePath
	if !strings.HasPrefix(dstPath, s.absBasePath) {
		return fmt.Errorf("invalid file path, potential directory traversal: %s", identifier)
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("failed to copy file content to '%s': %w", dstPath, err)
	}

	return nil
}

// Get 取得文件
func (s *localStorage) Get(identifier string) (io.ReadCloser, error) {
	if !isValidIdentifier(identifier) {
		return nil, fmt.Errorf("invalid file identifier: %s", identifier)
	}

	fullPath := filepath.Join(s.absBasePath, identifier)

	// 确保路径未越界
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return nil, fmt.Errorf("invalid file path, potential directory traversal: %s", identifier)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", identifier)
		}
		return nil, fmt.Errorf("failed to open file '%s': %w", identifier, err)
	}

	return file, nil
}

// Delete 删除文件
func (s *localStorage) Delete(identifier string) error {
	if !isValidIdentifier(identifier) {
		return fmt.Errorf("invalid file identifier: %s", identifier)
	}

	fullPath := filepath.Join(s.absBasePath, identifier)

	// 确保路径不越界
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return fmt.Errorf("invalid file path: %s", identifier)
	}

	err := os.Remove(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file to delete not found: %s", identifier)
		}
		return fmt.Errorf("failed to delete local file '%s': %w", fullPath, err)
	}

	return nil
}

// isValidIdentifier 校验 identifier 是否合法
func isValidIdentifier(identifier string) bool {
	// 拒绝空标识符
	if identifier == "" {
		return false
	}

	// 拒绝绝对路径
	if filepath.IsAbs(identifier) {
		return false
	}

	// 拒绝 ".."
	if strings.Contains(identifier, "..") {
		return false
	}

	// 只允许字母、数字、横线、下划线和点
	for _, r := range identifier {
		if !((r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.') {
			return false
		}
	}

	return true
}
