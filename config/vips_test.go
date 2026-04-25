package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoVipsCacheMemMB(t *testing.T) {
	tests := []struct {
		name       string
		limitBytes int64
		want       int
	}{
		{name: "unknown uses default", limitBytes: 0, want: defaultVipsCacheMemMB},
		{name: "small memory clamps to minimum", limitBytes: 512 * bytesPerMB, want: minAutoVipsCacheMemMB},
		{name: "two gigabytes", limitBytes: 2 * 1024 * bytesPerMB, want: 163},
		{name: "large memory clamps to maximum", limitBytes: 16 * 1024 * bytesPerMB, want: maxAutoVipsCacheMemMB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, autoVipsCacheMemMB(tt.limitBytes))
		})
	}
}

func TestGetVipsCacheConfigAuto(t *testing.T) {
	old := detectMemoryLimitSourceFunc
	detectMemoryLimitSourceFunc = func() (int64, string) {
		return 4 * 1024 * bytesPerMB, "cgroup"
	}
	t.Cleanup(func() { detectMemoryLimitSourceFunc = old })

	cfg := (&Config{}).GetVipsCacheConfig()

	assert.Equal(t, 327, cfg.MaxCacheMemMB)
	assert.Equal(t, 327*bytesPerMB, cfg.MaxCacheMem)
	assert.Equal(t, defaultVipsCacheSize, cfg.MaxCacheSize)
	assert.Equal(t, defaultVipsCacheFiles, cfg.MaxCacheFiles)
	assert.Equal(t, "cgroup", cfg.MemorySource)
}

func TestGetVipsCacheConfigOverrides(t *testing.T) {
	old := detectMemoryLimitSourceFunc
	detectMemoryLimitSourceFunc = func() (int64, string) {
		return 4 * 1024 * bytesPerMB, "cgroup"
	}
	t.Cleanup(func() { detectMemoryLimitSourceFunc = old })

	cfg := (&Config{
		VipsCacheMemMB: 128,
		VipsCacheSize:  64,
		VipsCacheFiles: 16,
	}).GetVipsCacheConfig()

	assert.Equal(t, 128, cfg.MaxCacheMemMB)
	assert.Equal(t, 128*bytesPerMB, cfg.MaxCacheMem)
	assert.Equal(t, 64, cfg.MaxCacheSize)
	assert.Equal(t, 16, cfg.MaxCacheFiles)
	assert.Equal(t, "config", cfg.MemorySource)
}

func TestReadMemoryLimitFile(t *testing.T) {
	dir := t.TempDir()

	limited := filepath.Join(dir, "memory.max")
	require.NoError(t, os.WriteFile(limited, []byte("1073741824\n"), 0o600))
	assert.Equal(t, int64(1024*bytesPerMB), readMemoryLimitFile(limited))

	unlimited := filepath.Join(dir, "unlimited")
	require.NoError(t, os.WriteFile(unlimited, []byte("max\n"), 0o600))
	assert.Equal(t, int64(0), readMemoryLimitFile(unlimited))

	huge := filepath.Join(dir, "huge")
	require.NoError(t, os.WriteFile(huge, []byte("4611686018427387904\n"), 0o600))
	assert.Equal(t, int64(0), readMemoryLimitFile(huge))
}

func TestReadSystemMemoryBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	require.NoError(t, os.WriteFile(path, []byte("MemTotal:        2048000 kB\nMemFree:          100000 kB\n"), 0o600))

	assert.Equal(t, int64(2048000*1024), readSystemMemoryBytes(path))
}
