package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStorageSaveWithContextMovesTempFileWhenPossible(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewLocalStorage(baseDir)
	require.NoError(t, err)

	srcFile, err := os.CreateTemp(baseDir, "upload-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srcFile.Close()
	})

	_, err = srcFile.WriteString("hello world")
	require.NoError(t, err)
	_, err = srcFile.Seek(0, 0)
	require.NoError(t, err)

	srcPath := srcFile.Name()
	err = store.SaveWithContext(context.Background(), "nested/final.txt", srcFile)
	require.NoError(t, err)

	dstPath := filepath.Join(baseDir, "nested", "final.txt")
	data, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	_, err = os.Stat(srcPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
