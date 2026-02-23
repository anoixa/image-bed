package worker

import (
	"context"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
)

// ImageProcessingConfig 图片处理配置
type ImageProcessingConfig struct {
	MaxConcurrentImages int   // 同时处理的最大图片数
	MaxImageSizeMB      int   // 最大图片大小(MB)
	GCTriggerThreshold  int64 // 触发GC的内存阈值(字节)
}

// DefaultImageProcessingConfig 默认配置
func DefaultImageProcessingConfig() *ImageProcessingConfig {
	return &ImageProcessingConfig{
		MaxConcurrentImages: 2,                 // 最多同时处理2张图片
		MaxImageSizeMB:      50,                // 50MB
		GCTriggerThreshold:  200 * 1024 * 1024, // 200MB
	}
}

// ImageProcessingSemaphore 图片处理信号量
type ImageProcessingSemaphore struct {
	semaphore chan struct{}
	config    *ImageProcessingConfig
}

var (
	globalSemaphore     *ImageProcessingSemaphore
	globalSemaphoreOnce sync.Once
)

// InitGlobalSemaphore 初始化全局信号量
func InitGlobalSemaphore(config *ImageProcessingConfig) {
	globalSemaphoreOnce.Do(func() {
		globalSemaphore = &ImageProcessingSemaphore{
			semaphore: make(chan struct{}, config.MaxConcurrentImages),
			config:    config,
		}
		log.Printf("[WorkerPool] Image processing semaphore initialized, max concurrent: %d", config.MaxConcurrentImages)
	})
}

// GetGlobalSemaphore 获取全局信号量
func GetGlobalSemaphore() *ImageProcessingSemaphore {
	if globalSemaphore == nil {
		InitGlobalSemaphore(DefaultImageProcessingConfig())
	}
	return globalSemaphore
}

// Acquire 获取信号量许可
func (s *ImageProcessingSemaphore) Acquire(ctx context.Context) error {
	select {
	case s.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 释放信号量许可
func (s *ImageProcessingSemaphore) Release() {
	select {
	case <-s.semaphore:
	default:
		log.Println("[WorkerPool] Warning: releasing unacquired semaphore")
	}
}

// min 返回较小的整数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PoolStats 任务池统计信息
type PoolStats struct {
	Submitted   uint64 // 已提交任务数
	Executed    uint64 // 已执行任务数
	Failed      uint64 // 失败任务数（panic）
	QueueSize   int    // 当前队列长度
	QueueCap    int    // 队列容量
	WorkerCount int    // Worker 数量
}

// Pool 独立的异步任务池
type Pool struct {
	taskCh   chan func()
	wg       sync.WaitGroup
	isClosed atomic.Bool

	submittedCount atomic.Uint64
	executedCount  atomic.Uint64
	failedCount    atomic.Uint64
	workerCount    int
	queueCap       int
}

var (
	globalPool     *Pool
	globalPoolOnce sync.Once
)

// InitGlobalPool 初始化全局任务池
func InitGlobalPool(workers, queueSize int) {
	globalPoolOnce.Do(func() {
		globalPool = NewPool(workers, queueSize)
	})
}

// GetGlobalPool 获取全局任务池实例
func GetGlobalPool() *Pool {
	return globalPool
}

// StopGlobalPool 全局平滑退出入口
func StopGlobalPool() {
	if globalPool != nil {
		globalPool.Stop()
	}
}

// NewPool 创建新的任务池
func NewPool(workers, queueSize int) *Pool {
	if queueSize <= 0 {
		queueSize = 100 // 降低队列大小以减少内存占用（原 1000）
	}
	if workers <= 0 {
		// 限制最大 worker 数量，避免高 CPU 核心服务器占用过多内存
		workers = min(runtime.NumCPU(), 4) // 最多 4 个 worker（原 CPU*2 无上限）
	}

	p := &Pool{
		taskCh:   make(chan func(), queueSize),
		queueCap: queueSize,
	}

	InitGlobalSemaphore(DefaultImageProcessingConfig())

	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	p.workerCount = workers
	log.Printf("[WorkerPool] Started with %d workers, queue size %d", workers, queueSize)
	return p
}

// worker 是实际执行任务的 Goroutine
func (p *Pool) worker() {
	defer p.wg.Done()
	for task := range p.taskCh {
		if task != nil {
			p.executeTaskWithRecovery(task)
		}
	}
}

// executeTaskWithRecovery 包装单个任务
func (p *Pool) executeTaskWithRecovery(task func()) {
	p.executedCount.Add(1)
	defer func() {
		if r := recover(); r != nil {
			p.failedCount.Add(1)
			log.Printf("[WorkerPool] Task panicked: %v", r)
		}
	}()
	task()
}

// Submit 提交异步任务到队列
// 返回 false 表示队列已满或池已关闭
func (p *Pool) Submit(task func()) (ok bool) {
	if p.isClosed.Load() {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	select {
	case p.taskCh <- task:
		p.submittedCount.Add(1)
		return true
	default:
		log.Printf("[WorkerPool] Task queue full, dropping task")
		return false
	}
}

// Stop 关闭池
func (p *Pool) Stop() {
	if p.isClosed.CompareAndSwap(false, true) {
		log.Println("[WorkerPool] Stopping...")
		close(p.taskCh)
		p.wg.Wait()
		log.Println("[WorkerPool] Stopped gracefully.")
	}
}

// GetStats 获取任务池当前的运行状态
func (p *Pool) GetStats() PoolStats {
	return PoolStats{
		Submitted:   p.submittedCount.Load(),
		Executed:    p.executedCount.Load(),
		Failed:      p.failedCount.Load(),
		QueueSize:   len(p.taskCh),
		QueueCap:    p.queueCap,
		WorkerCount: p.workerCount,
	}
}
