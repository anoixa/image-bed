package storage

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRemainingReaderSize(t *testing.T) {
	t.Run("ReturnsUnknownForNonSeeker", func(t *testing.T) {
		size, err := getRemainingReaderSize(bytes.NewBufferString("hello"))
		require.NoError(t, err)
		assert.Equal(t, int64(-1), size)
	})

	t.Run("UsesRemainingBytesFromCurrentPosition", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sample.txt")
		require.NoError(t, os.WriteFile(path, []byte("hello world"), 0600))

		file, err := os.Open(path)
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		_, err = file.Seek(6, io.SeekStart)
		require.NoError(t, err)

		size, err := getRemainingReaderSize(file)
		require.NoError(t, err)
		assert.Equal(t, int64(5), size)

		current, err := file.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(6), current, "size detection must restore reader position")
	})
}
