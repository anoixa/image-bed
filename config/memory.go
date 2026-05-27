package config

import (
	"errors"
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
