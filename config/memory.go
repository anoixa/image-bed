package config

import (
	"errors"
	"runtime"
)

// 默认内存限制 512MB
const defaultMemoryLimitMB = 512

// ErrMemoryLimitExceeded 内存限制超出错误
var ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

// GetWorkerMemoryLimitMB 返回 Worker 内存限制（MB），默认 512
func (c *Config) GetWorkerMemoryLimitMB() int {
	if c.WorkerMemoryLimitMB <= 0 {
		return defaultMemoryLimitMB
	}
	return c.WorkerMemoryLimitMB
}

// SetWorkerMemoryLimitMB 设置 Worker 内存限制（MB）
func (c *Config) SetWorkerMemoryLimitMB(limitMB int) {
	if limitMB > 0 {
		c.WorkerMemoryLimitMB = limitMB
	}
}

// CheckMemoryLimit 检查当前内存是否超过 Worker 内存限制
func (c *Config) CheckMemoryLimit() error {
	limit := c.GetWorkerMemoryLimitMB()
	if limit <= 0 {
		return nil // 无限制
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentMB := float64(m.HeapAlloc) / 1024 / 1024
	if currentMB >= float64(limit) {
		return ErrMemoryLimitExceeded
	}
	return nil
}

// CheckMemoryLimitWithGC 检查内存限制，如果超过则先尝试 GC
func (c *Config) CheckMemoryLimitWithGC() error {
	limit := c.GetWorkerMemoryLimitMB()
	if limit <= 0 {
		return nil // 无限制
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentMB := float64(m.HeapAlloc) / 1024 / 1024
	if currentMB < float64(limit) {
		return nil
	}

	runtime.GC()

	runtime.ReadMemStats(&m)
	currentMB = float64(m.HeapAlloc) / 1024 / 1024
	if currentMB >= float64(limit) {
		return ErrMemoryLimitExceeded
	}
	return nil
}
