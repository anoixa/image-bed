package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Storage 本地文件存储实现
type Storage struct {
	absBasePath string
}

// NewStorage 创建本地存储提供者
func NewStorage(basePath string) (*Storage, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for '%s': %w", basePath, err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local storage directory '%s': %w", absPath, err)
	}

	return &Storage{
		absBasePath: absPath + string(os.PathSeparator),
	}, nil
}

// SaveWithContext 保存文件到本地存储
func (s *Storage) SaveWithContext(ctx context.Context, identifier string, file io.Reader) error {
	if !IsValidIdentifier(identifier) {
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

// GetWithContext 从本地存储获取文件
func (s *Storage) GetWithContext(ctx context.Context, identifier string) (io.ReadSeeker, error) {
	if !IsValidIdentifier(identifier) {
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

// DeleteWithContext 从本地存储删除文件
func (s *Storage) DeleteWithContext(ctx context.Context, identifier string) error {
	if !IsValidIdentifier(identifier) {
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

// Exists 检查文件是否存在
func (s *Storage) Exists(ctx context.Context, identifier string) (bool, error) {
	if !IsValidIdentifier(identifier) {
		return false, fmt.Errorf("invalid file identifier: %s", identifier)
	}

	fullPath := filepath.Join(s.absBasePath, identifier)
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return false, fmt.Errorf("invalid file path: %s", identifier)
	}

	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Health 检查存储健康状态
func (s *Storage) Health(ctx context.Context) error {
	_, err := os.ReadDir(s.absBasePath)
	return err
}

// Name 返回存储名称
func (s *Storage) Name() string {
	return "local"
}

// BasePath 返回存储的基础路径
func (s *Storage) BasePath() string {
	return s.absBasePath
}

// IsValidIdentifier 校验 identifier 是否合法
func IsValidIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}

	if filepath.IsAbs(identifier) {
		return false
	}

	if strings.Contains(identifier, "..") {
		return false
	}

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
