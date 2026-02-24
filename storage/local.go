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
	"unicode"
)

// LocalStorage 本地文件存储实现
type LocalStorage struct {
	absBasePath string
}

// BasePath 返回存储的基础路径
func (s *LocalStorage) BasePath() string {
	return s.absBasePath
}

// NewLocalStorage 创建本地存储提供者
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// 验证路径不为空
	if basePath == "" {
		return nil, fmt.Errorf("base path cannot be empty")
	}

	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for '%s': %w", basePath, err)
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if err = os.MkdirAll(absPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create local storage directory '%s': %w", absPath, err)
		}
		resolvedPath, err = filepath.EvalSymlinks(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate symlinks for '%s': %w", absPath, err)
		}
	}
	absPath = resolvedPath

	// 写权限测试
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

// validatePath 统一的路径验证和安全路径生成
func (s *LocalStorage) validatePath(storagePath string) (string, error) {
	if storagePath == "" {
		return "", fmt.Errorf("storage path is empty")
	}

	for _, r := range storagePath {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("storage path contains control characters")
		}
	}

	if filepath.IsAbs(storagePath) {
		return "", fmt.Errorf("storage path must be relative")
	}

	fullPath := filepath.Join(s.absBasePath, storagePath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	if !strings.HasPrefix(absPath, s.absBasePath) {
		return "", fmt.Errorf("invalid path: directory traversal detected")
	}

	return absPath, nil
}

// SaveWithContext 保存文件到本地存储
func (s *LocalStorage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	dstPath, err := s.validatePath(storagePath)
	if err != nil {
		return fmt.Errorf("invalid storage path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err = io.Copy(dst, file); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// GetWithContext 从本地存储获取文件
func (s *LocalStorage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath, err := s.validatePath(storagePath)
	if err != nil {
		return nil, fmt.Errorf("invalid storage path: %w", err)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// OpenFile 打开本地文件（用于零拷贝传输）
func (s *LocalStorage) OpenFile(ctx context.Context, storagePath string) (*os.File, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath, err := s.validatePath(storagePath)
	if err != nil {
		return nil, fmt.Errorf("invalid storage path: %w", err)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// DeleteWithContext 从本地存储删除文件
func (s *LocalStorage) DeleteWithContext(ctx context.Context, storagePath string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath, err := s.validatePath(storagePath)
	if err != nil {
		return fmt.Errorf("invalid storage path: %w", err)
	}

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在也算成功
		}
		return fmt.Errorf("failed to remove file: %w", err)
	}

	return nil
}

// Exists 检查文件是否存在
func (s *LocalStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	fullPath, err := s.validatePath(storagePath)
	if err != nil {
		return false, fmt.Errorf("invalid storage path: %w", err)
	}

	_, err = os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check file existence: %w", err)
	}

	return true, nil
}

// Health 检查存储健康状态
func (s *LocalStorage) Health(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 检查目录是否可访问
	if _, err := os.Stat(s.absBasePath); err != nil {
		return fmt.Errorf("storage directory not accessible: %w", err)
	}

	// 测试写权限
	testFile := filepath.Join(s.absBasePath, ".health_check_"+strconv.FormatInt(time.Now().UnixNano(), 10))
	f, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("storage directory not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(testFile)

	return nil
}

// Name 返回存储名称
func (s *LocalStorage) Name() string {
	return "local"
}

// 为了兼容性，保留原有的方法名
// Save 保存文件到本地存储（已废弃，请使用 SaveWithContext）
func (s *LocalStorage) Save(storagePath string, file io.Reader) error {
	return s.SaveWithContext(context.Background(), storagePath, file)
}

// Get 从本地存储获取文件（已废弃，请使用 GetWithContext）
func (s *LocalStorage) Get(storagePath string) (io.ReadSeeker, error) {
	return s.GetWithContext(context.Background(), storagePath)
}

// Delete 从本地存储删除文件（已废弃，请使用 DeleteWithContext）
func (s *LocalStorage) Delete(storagePath string) error {
	return s.DeleteWithContext(context.Background(), storagePath)
}
