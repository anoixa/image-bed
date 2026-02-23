package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalStorage 本地文件存储实现
type LocalStorage struct {
	absBasePath string
}

// NewLocalStorage 创建本地存储提供者
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for '%s': %w", basePath, err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local storage directory '%s': %w", absPath, err)
	}

	testFile := filepath.Join(absPath, ".write_test_"+strconv.FormatInt(time.Now().UnixNano(), 10))
	f, err := os.Create(testFile)
	if err != nil {
		return nil, fmt.Errorf("local storage directory '%s' is not writable: %w", absPath, err)
	}
	_ = f.Close()
	_ = os.Remove(testFile)

	return &LocalStorage{
		absBasePath: absPath + string(os.PathSeparator),
	}, nil
}

// SaveWithContext 保存文件到本地存储
// storagePath: 存储路径，如 original/2024/01/15/a1b2c3d4e5f6.jpg
func (s *LocalStorage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	if !IsValidStoragePath(storagePath) {
		return fmt.Errorf("invalid storage path: %s", storagePath)
	}

	dstPath := filepath.Join(s.absBasePath, storagePath)

	if !strings.HasPrefix(dstPath, s.absBasePath) {
		return fmt.Errorf("invalid file path, potential directory traversal: %s", storagePath)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for '%s': %w", storagePath, err)
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dstPath, err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("failed to copy file content to '%s': %w", dstPath, err)
	}

	return nil
}

// GetWithContext 从本地存储获取文件
func (s *LocalStorage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	if !IsValidStoragePath(storagePath) {
		return nil, fmt.Errorf("invalid storage path: %s", storagePath)
	}

	fullPath := filepath.Join(s.absBasePath, storagePath)

	// 防止目录遍历攻击
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return nil, fmt.Errorf("invalid file path, potential directory traversal: %s", storagePath)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", storagePath)
		}
		return nil, fmt.Errorf("failed to open file '%s': %w", storagePath, err)
	}

	return file, nil
}

// OpenFile 零拷贝传输
func (s *LocalStorage) OpenFile(ctx context.Context, storagePath string) (*os.File, error) {
	if !IsValidStoragePath(storagePath) {
		return nil, fmt.Errorf("invalid storage path: %s", storagePath)
	}

	fullPath := filepath.Join(s.absBasePath, storagePath)
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return nil, fmt.Errorf("invalid file path: %s", storagePath)
	}

	return os.Open(fullPath)
}

// DeleteWithContext 从本地存储删除文件
func (s *LocalStorage) DeleteWithContext(ctx context.Context, storagePath string) error {
	if !IsValidStoragePath(storagePath) {
		return fmt.Errorf("invalid storage path: %s", storagePath)
	}

	fullPath := filepath.Join(s.absBasePath, storagePath)

	// 防止目录遍历攻击
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return fmt.Errorf("invalid file path: %s", storagePath)
	}

	err := os.Remove(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file to delete not found: %s", storagePath)
		}
		return fmt.Errorf("failed to delete local file '%s': %w", fullPath, err)
	}

	return nil
}

// Exists 检查文件是否存在
func (s *LocalStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	if !IsValidStoragePath(storagePath) {
		return false, fmt.Errorf("invalid storage path: %s", storagePath)
	}

	fullPath := filepath.Join(s.absBasePath, storagePath)
	if !strings.HasPrefix(fullPath, s.absBasePath) {
		return false, fmt.Errorf("invalid file path: %s", storagePath)
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
func (s *LocalStorage) Health(ctx context.Context) error {
	_, err := os.ReadDir(s.absBasePath)
	return err
}

// Name 返回存储名称
func (s *LocalStorage) Name() string {
	return "local"
}

// BasePath 返回存储的基础路径
func (s *LocalStorage) BasePath() string {
	return s.absBasePath
}

// IsValidStoragePath 校验存储路径是否合法
func IsValidStoragePath(path string) bool {
	if path == "" {
		return false
	}

	// 不允许绝对路径
	if filepath.IsAbs(path) {
		return false
	}

	// 防止目录遍历
	if strings.Contains(path, "..") {
		return false
	}

	// 只允许安全字符
	for _, r := range path {
		if (r < 'a' || r > 'z') &&
			(r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') &&
			r != '-' && r != '_' && r != '.' && r != '/' {
			return false
		}
	}

	return true
}
