package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProcStatusRSSBytes(t *testing.T) {
	data := "Name:\timage-bed\nVmRSS:\t  115780 kB\nVmSize:\t4061732 kB\n"

	rssBytes, err := parseProcStatusRSSBytes(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(115780*1024), rssBytes)
}

func TestParseProcStatusMemoryInfo(t *testing.T) {
	data := "Name:\timage-bed\nVmRSS:\t  115780 kB\nRssAnon:\t   90212 kB\nRssFile:\t   25568 kB\n"

	info, err := parseProcStatusMemoryInfo(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(115780*1024), info.RSSBytes)
	assert.Equal(t, uint64(90212*1024), info.RssAnonBytes)
	assert.Equal(t, uint64(25568*1024), info.RssFileBytes)
}

func TestParseProcStatusRSSBytesMissingField(t *testing.T) {
	_, err := parseProcStatusRSSBytes("Name:\timage-bed\nVmSize:\t4061732 kB\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VmRSS not found")
}

func TestReadStatmRSSBytesUsesProvidedPath(t *testing.T) {
	tempDir := t.TempDir()
	statmPath := filepath.Join(tempDir, "statm")
	require.NoError(t, os.WriteFile(statmPath, []byte("100 25 0 0 0 0 0\n"), 0o644))

	rssBytes, err := readStatmRSSBytes(statmPath)
	require.NoError(t, err)
	assert.Equal(t, uint64(25*os.Getpagesize()), rssBytes)
}
