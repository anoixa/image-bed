package utils

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/davidbyttow/govips/v2/vips"
)

// MemoryStats 内存统计
type MemoryStats struct {
	HeapAllocMB   float64
	HeapSysMB     float64
	HeapInUseMB   float64
	StackSysMB    float64
	TotalAllocMB  float64
	RSSMB         float64
	NumGC         uint32
	GCSysMB       float64
	LastGCTime    time.Time
	Goroutines    int
	VipsMemMB     float64
	VipsMemHighMB float64
	VipsAllocs    int64
	VipsOpenFiles int64
}

// bytesToMB 将字节转换为 MB
func bytesToMB(bytes uint64) float64 {
	return float64(bytes) / 1024 / 1024
}

// GetMemoryStats 获取当前内存统计
func GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rssBytes, _ := readProcessRSSBytes()
	vipsStats := getVipsMemoryStats()

	return MemoryStats{
		HeapAllocMB:   bytesToMB(m.HeapAlloc),
		HeapSysMB:     bytesToMB(m.HeapSys),
		HeapInUseMB:   bytesToMB(m.HeapInuse),
		StackSysMB:    bytesToMB(m.StackSys),
		TotalAllocMB:  bytesToMB(m.TotalAlloc),
		RSSMB:         bytesToMB(rssBytes),
		NumGC:         m.NumGC,
		GCSysMB:       bytesToMB(m.GCSys),
		LastGCTime:    time.Unix(0, int64(m.LastGC)),
		Goroutines:    runtime.NumGoroutine(),
		VipsMemMB:     bytesToMB(uint64(vipsStats.Mem)),
		VipsMemHighMB: bytesToMB(uint64(vipsStats.MemHigh)),
		VipsAllocs:    vipsStats.Allocs,
		VipsOpenFiles: vipsStats.Files,
	}
}

// LogMemoryStats 记录内存统计（仅在 dev 环境输出）
func LogMemoryStats(prefix string) {
	if !config.IsDevelopment() {
		return
	}
	stats := GetMemoryStats()
	Debugf("[Memory][%s] HeapAlloc=%.2fMB, HeapSys=%.2fMB, HeapInUse=%.2fMB, Stack=%.2fMB, Goroutines=%d, NumGC=%d",
		prefix,
		stats.HeapAllocMB,
		stats.HeapSysMB,
		stats.HeapInUseMB,
		stats.StackSysMB,
		stats.Goroutines,
		stats.NumGC,
	)
}

// GetNumCPU 获取 CPU 核心数
func GetNumCPU() int {
	return runtime.NumCPU()
}

// FormatBytes 将字节格式化为人类可读字符串
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

type vipsMemoryStats struct {
	Mem     int64
	MemHigh int64
	Files   int64
	Allocs  int64
}

func getVipsMemoryStats() (stats vipsMemoryStats) {
	defer func() {
		if recover() != nil {
			stats = vipsMemoryStats{}
		}
	}()

	var vipsStats vips.MemoryStats
	vips.ReadVipsMemStats(&vipsStats)

	return vipsMemoryStats{
		Mem:     max(vipsStats.Mem, 0),
		MemHigh: max(vipsStats.MemHigh, 0),
		Files:   max(vipsStats.Files, 0),
		Allocs:  max(vipsStats.Allocs, 0),
	}
}

func max(v, floor int64) int64 {
	if v < floor {
		return floor
	}
	return v
}

func ReadProcessRSS() (float64, error) {
	rssBytes, err := readProcessRSSBytes()
	if err != nil {
		return 0, err
	}
	return bytesToMB(rssBytes), nil
}

func readProcessRSSBytes() (uint64, error) {
	rssBytes, err := readProcStatusRSSBytes("/proc/self/status")
	if err == nil {
		return rssBytes, nil
	}

	return readStatmRSSBytes("/proc/self/statm")
}

func readProcStatusRSSBytes(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	return parseProcStatusRSSBytes(string(data))
}

func parseProcStatusRSSBytes(data string) (uint64, error) {
	for _, line := range strings.Split(data, "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("invalid VmRSS line: %q", line)
		}

		valueKB, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmRSS value: %w", err)
		}

		return valueKB * 1024, nil
	}

	return 0, fmt.Errorf("VmRSS not found")
}

func readStatmRSSBytes(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	var totalPages, residentPages uint64
	if _, err := fmt.Sscanf(string(data), "%d %d", &totalPages, &residentPages); err != nil {
		return 0, err
	}

	return residentPages * uint64(os.Getpagesize()), nil
}
