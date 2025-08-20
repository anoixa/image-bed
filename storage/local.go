package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// localStorage 本地文件存储。
type localStorage struct {
	basePath string
}

// newLocalStorage
func newLocalStorage(basePath string) *localStorage {
	if err := os.MkdirAll(basePath, os.ModePerm); err != nil {
		panic(fmt.Sprintf("failed to create local storage directory '%s': %v", basePath, err))
	}
	return &localStorage{basePath: basePath}
}

// Save
func (s *localStorage) Save(identifier string, file io.Reader) error {
	dstPath := filepath.Join(s.basePath, identifier)

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

// Get Get image
func (s *localStorage) Get(identifier string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.basePath, identifier)

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", identifier)
		}
		return nil, fmt.Errorf("failed to open file '%s': %w", identifier, err)
	}

	return file, nil
}
