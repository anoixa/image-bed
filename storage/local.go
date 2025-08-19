package storage

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"mime/multipart"
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
func (s *localStorage) Save(file multipart.File, header *multipart.FileHeader) (string, error) {
	fileExt := filepath.Ext(header.Filename)
	uniqueFileName := uuid.New().String() + fileExt
	dstPath := filepath.Join(s.basePath, uniqueFileName)

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("failed to copy file content: %w", err)
	}
	return uniqueFileName, nil
}

// Get
func (s *localStorage) Get(filename string) (string, error) {
	fullPath := filepath.Join(s.basePath, filename)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", fmt.Errorf("file not found: %s", filename)
	}
	return fullPath, nil
}
