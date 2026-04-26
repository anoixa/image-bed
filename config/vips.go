package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	bytesPerMB = 1024 * 1024

	defaultVipsCacheMemMB   = 64
	minAutoVipsCacheMemMB   = 32
	maxAutoVipsCacheMemMB   = 128
	autoVipsCachePercent    = 2
	defaultVipsCacheSize    = 50
	defaultVipsCacheFiles   = 0
	maxSaneMemoryLimitBytes = int64(1 << 60)
)

var (
	cgroupV2MemoryMaxPath       = "/sys/fs/cgroup/memory.max"
	cgroupV1MemoryLimitPath     = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	procMeminfoPath             = "/proc/meminfo"
	detectMemoryLimitBytesFunc  = detectMemoryLimitBytes
	detectMemoryLimitSourceFunc = detectMemoryLimitSource
)

type VipsCacheConfig struct {
	MaxCacheMem      int
	MaxCacheMemMB    int
	MaxCacheSize     int
	MaxCacheFiles    int
	MemoryLimitBytes int64
	MemorySource     string
}

func (c *Config) GetVipsCacheConfig() VipsCacheConfig {
	limitBytes, source := detectMemoryLimitSourceFunc()
	cacheMemMB := autoVipsCacheMemMB(limitBytes)
	if c.VipsCacheMemMB > 0 {
		cacheMemMB = c.VipsCacheMemMB
		source = "config"
	}

	cacheSize := defaultVipsCacheSize
	if c.VipsCacheSize > 0 {
		cacheSize = c.VipsCacheSize
	}

	cacheFiles := defaultVipsCacheFiles
	if c.VipsCacheFiles > 0 {
		cacheFiles = c.VipsCacheFiles
	}

	return VipsCacheConfig{
		MaxCacheMem:      cacheMemMB * bytesPerMB,
		MaxCacheMemMB:    cacheMemMB,
		MaxCacheSize:     cacheSize,
		MaxCacheFiles:    cacheFiles,
		MemoryLimitBytes: limitBytes,
		MemorySource:     source,
	}
}

func autoVipsCacheMemMB(limitBytes int64) int {
	if limitBytes <= 0 {
		return defaultVipsCacheMemMB
	}

	limitMB := limitBytes / bytesPerMB
	cacheMB := int(limitMB * autoVipsCachePercent / 100)
	if cacheMB < minAutoVipsCacheMemMB {
		return minAutoVipsCacheMemMB
	}
	if cacheMB > maxAutoVipsCacheMemMB {
		return maxAutoVipsCacheMemMB
	}
	return cacheMB
}

func detectMemoryLimitSource() (int64, string) {
	if limit := detectMemoryLimitBytesFunc(); limit > 0 {
		return limit, "cgroup"
	}
	if limit := readSystemMemoryBytes(procMeminfoPath); limit > 0 {
		return limit, "system"
	}
	return 0, "default"
}

func detectMemoryLimitBytes() int64 {
	if limit := readMemoryLimitFile(cgroupV2MemoryMaxPath); limit > 0 {
		return limit
	}
	if limit := readMemoryLimitFile(cgroupV1MemoryLimitPath); limit > 0 {
		return limit
	}
	return 0
}

func readMemoryLimitFile(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "max" {
		return 0
	}

	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit <= 0 || limit >= maxSaneMemoryLimitBytes {
		return 0
	}
	return limit
}

func readSystemMemoryBytes(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			return 0
		}
		return kb * 1024
	}
	return 0
}

func (v VipsCacheConfig) String() string {
	memoryLimit := "unknown"
	if v.MemoryLimitBytes > 0 {
		memoryLimit = fmt.Sprintf("%dMB", v.MemoryLimitBytes/bytesPerMB)
	}
	return fmt.Sprintf(
		"cache_mem=%dMB cache_size=%d cache_files=%d memory_source=%s memory_limit=%s",
		v.MaxCacheMemMB,
		v.MaxCacheSize,
		v.MaxCacheFiles,
		v.MemorySource,
		memoryLimit,
	)
}
