package tempfiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSweepOrphansDeletesOnlyKnownOldTempFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	oldKnown := filepath.Join(dir, "upload-stream-old")
	newKnown := filepath.Join(dir, "variant-output-new")
	oldUnknown := filepath.Join(dir, "user-data-old")

	require.NoError(t, os.WriteFile(oldKnown, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(newKnown, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(oldUnknown, []byte("unknown"), 0o600))

	oldTime := now.Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(oldKnown, oldTime, oldTime))
	require.NoError(t, os.Chtimes(oldUnknown, oldTime, oldTime))

	stats, err := SweepOrphans(context.Background(), dir, time.Hour, now)
	require.NoError(t, err)

	assert.Equal(t, 2, stats.Checked)
	assert.Equal(t, 1, stats.Deleted)

	_, err = os.Stat(oldKnown)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(newKnown)
	assert.NoError(t, err)
	_, err = os.Stat(oldUnknown)
	assert.NoError(t, err)
}
