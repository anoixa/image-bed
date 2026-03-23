package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProcessingFilePath_LocalStorage(t *testing.T) {
	dir := t.TempDir()
	ls, err := storage.NewLocalStorage(dir)
	require.NoError(t, err)

	relPath := "img/test.jpg"
	full := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte("fakejpeg"), 0600))

	task := &ImagePipelineTask{
		StoragePath: relPath,
		Storage:     ls,
	}

	path, cleanup, err := task.getProcessingFilePath(context.Background())
	require.NoError(t, err)

	assert.Equal(t, full, path)

	// call cleanup explicitly first, then verify local file is untouched
	cleanup()
	_, statErr := os.Stat(full)
	assert.NoError(t, statErr, "local storage file must not be deleted by cleanup")
}
