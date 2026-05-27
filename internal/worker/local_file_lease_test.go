package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFileLeaseTransferAndConsumeOwnsCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload.tmp")
	require.NoError(t, os.WriteFile(path, []byte("body"), 0o600))

	lease := NewLocalFileLease(path)
	require.True(t, lease.Transfer())
	lease.CleanupIfRequestOwned()
	assert.FileExists(t, path)

	consumedPath, cleanup, ok := lease.Consume()
	require.True(t, ok)
	assert.Equal(t, path, consumedPath)

	cleanup()
	_, err := os.Stat(path)
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestLocalFileLeaseRequestCleanupWinsBeforeTransfer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload.tmp")
	require.NoError(t, os.WriteFile(path, []byte("body"), 0o600))

	lease := NewLocalFileLease(path)
	lease.CleanupIfRequestOwned()

	assert.False(t, lease.Transfer())
	_, _, ok := lease.Consume()
	assert.False(t, ok)

	_, err := os.Stat(path)
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}
