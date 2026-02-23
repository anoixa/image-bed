package utils

import (
	"errors"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/anoixa/image-bed/config"
)

// 默认内存限制 512MB
const defaultMemoryLimitMB = 512

// MemoryLimitExceeded 内存限制超出错误（类似 PHP memory_limit）
var MemoryLimitExceeded = errors.New("memory limit exceeded")

// 内存限制变量（线程安全）
var (
	memoryLimitMB    = defaultMemoryLimitMB
	memoryLimitMutex sync.RWMutex
)

// GetMemoryLimitMB 获取当前内存限制（MB）
func GetMemoryLimitMB() int {
	memoryLimitMutex.RLock()
	defer memoryLimitMutex.RUnlock()
	return memoryLimitMB
}

// SetMemoryLimitMB 设置内存限制（MB）
func SetMemoryLimitMB(limitMB int) {
	memoryLimitMutex.Lock()
	defer memoryLimitMutex.Unlock()
	if limitMB > 0 {
		memoryLimitMB = limitMB
	}
}

// CheckMemoryLimit 检查当前内存是否超过限制
// 返回 error 如果内存使用超过限制
func CheckMemoryLimit() error {
	limit := GetMemoryLimitMB()
	if limit <= 0 {
		return nil // 无限制
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 使用 HeapAlloc 作为当前内存使用量
	currentMB := float64(m.HeapAlloc) / 1024 / 1024
	if currentMB >= float64(limit) {
		return MemoryLimitExceeded
	}
	return nil
}

// CheckMemoryLimitWithGC 检查内存限制，如果超过则先尝试 GC
// 如果 GC 后仍然超过限制，返回错误
func CheckMemoryLimitWithGC() error {
	limit := GetMemoryLimitMB()
	if limit <= 0 {
		return nil // 无限制
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentMB := float64(m.HeapAlloc) / 1024 / 1024
	if currentMB < float64(limit) {
		return nil
	}

	// 超过限制，尝试 GC
	ForceGC()

	// 重新检查
	runtime.ReadMemStats(&m)
	currentMB = float64(m.HeapAlloc) / 1024 / 1024
	if currentMB >= float64(limit) {
		return MemoryLimitExceeded
	}
	return nil
}

// GetMemoryUsageMB 获取当前内存使用量（MB）
func GetMemoryUsageMB() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / 1024 / 1024
}

// MemoryStats 内存统计
type MemoryStats struct {
	HeapAllocMB float64
	HeapSysMB   float64
	HeapInUseMB float64
	StackSysMB  float64
	NumGC       uint32
	GCSysMB     float64
	LastGCTime  time.Time
	Goroutines  int
}

// bytesToMB 将字节转换为 MB
func bytesToMB(bytes uint64) float64 {
	return float64(bytes) / 1024 / 1024
}

// GetMemoryStats 获取当前内存统计
func GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemoryStats{
		HeapAllocMB: bytesToMB(m.HeapAlloc),
		HeapSysMB:   bytesToMB(m.HeapSys),
		HeapInUseMB: bytesToMB(m.HeapInuse),
		StackSysMB:  bytesToMB(m.StackSys),
		NumGC:       m.NumGC,
		GCSysMB:     bytesToMB(m.GCSys),
		LastGCTime:  time.Unix(0, int64(m.LastGC)),
		Goroutines:  runtime.NumGoroutine(),
	}
}

// LogMemoryStats 记录内存统计（仅在 dev 环境输出）
func LogMemoryStats(prefix string) {
	if config.CommitHash != "n/a" {
		return
	}
	stats := GetMemoryStats()
	log.Printf("[Memory][%s] HeapAlloc=%.2fMB, HeapSys=%.2fMB, HeapInUse=%.2fMB, Stack=%.2fMB, Goroutines=%d, NumGC=%d",
		prefix,
		stats.HeapAllocMB,
		stats.HeapSysMB,
		stats.HeapInUseMB,
		stats.StackSysMB,
		stats.Goroutines,
		stats.NumGC,
	)
}

// LogMemoryDiff 记录内存变化（仅在 dev 环境输出）
func LogMemoryDiff(prefix string, before MemoryStats) {
	if config.CommitHash != "n/a" {
		return
	}
	after := GetMemoryStats()
	deltaHeap := after.HeapAllocMB - before.HeapAllocMB
	log.Printf("[Memory][%s] Delta=%+.2fMB (Before=%.2fMB, After=%.2fMB), Goroutines=%d",
		prefix,
		deltaHeap,
		before.HeapAllocMB,
		after.HeapAllocMB,
		after.Goroutines,
	)
}

// ForceGC 强制垃圾回收
func ForceGC() {
	runtime.GC()
	debug.FreeOSMemory()
}

// MonitorMemory 内存监控函数，用于在任务前后打印内存变化
func MonitorMemory(operation string) func() {
	if config.CommitHash != "n/a" {
		return func() {}
	}
	before := GetMemoryStats()
	LogMemoryStats(operation + "[BEFORE]")

	return func() {
		LogMemoryDiff(operation+"[AFTER]", before)
	}
}
