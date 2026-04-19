package worker

import (
	"context"
	"fmt"
	"github.com/anoixa/image-bed/internal/vipsfile"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	appconfig "github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
)

var (
	globalSemaphore     *ImageProcessingSemaphore
	globalSemaphoreOnce sync.Once
	workerPoolLog       = utils.ForModule("WorkerPool")
)

type InFlightTaskSnapshot struct {
	ImageID    uint
	VariantIDs []uint
}

type inFlightTaskRegistry struct {
	mu     sync.Mutex
	nextID uint64
	tasks  map[uint64]InFlightTaskSnapshot
}

type inFlightTaskLease struct {
	registry *inFlightTaskRegistry
	id       uint64
}

var (
	globalPool     *Pool
	globalPoolOnce sync.Once
	inFlightTasks  = inFlightTaskRegistry{
		tasks: make(map[uint64]InFlightTaskSnapshot),
	}
)

var workerMemoryCheck = func() error {
	cfg := appconfig.Get()
	limit := cfg.GetWorkerMemoryLimitMB()
	if limit <= 0 {
		return nil
	}

	checkOnce := func() (float64, utils.MemoryStats) {
		stats := utils.GetMemoryStats()
		effectiveMB := effectiveWorkerMemoryMB(stats)
		return effectiveMB, stats
	}

	effectiveMB, _ := checkOnce()
	if effectiveMB < float64(limit) {
		return nil
	}

	runtime.GC()

	effectiveMB, stats := checkOnce()
	if effectiveMB >= float64(limit) {
		return fmt.Errorf(
			"%w: effective=%.2fMB active threshold=%dMB heap_in_use=%.2fMB heap_alloc=%.2fMB rss=%.2fMB vips=%.2fMB",
			appconfig.ErrMemoryLimitExceeded,
			effectiveMB,
			limit,
			stats.HeapInUseMB,
			stats.HeapAllocMB,
			stats.RSSMB,
			stats.VipsMemMB,
		)
	}

	return nil
}

func effectiveWorkerMemoryMB(stats utils.MemoryStats) float64 {
	effectiveMB := stats.HeapInUseMB + stats.VipsMemMB
	if effectiveMB < stats.HeapAllocMB+stats.VipsMemMB {
		effectiveMB = stats.HeapAllocMB + stats.VipsMemMB
	}
	return effectiveMB
}

// ImageProcessingConfig 图片处理配置
type ImageProcessingConfig struct {
	MaxConcurrentImages int   // 同时处理的最大图片数
	MaxImageSizeMB      int   // 最大图片大小(MB)
	GCTriggerThreshold  int64 // 触发GC的内存阈值(字节)
}

// ImageProcessingSemaphore 图片处理信号量
type ImageProcessingSemaphore struct {
	semaphore chan struct{}
	config    *ImageProcessingConfig
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
	doneCh   chan struct{}

	submittedCount atomic.Uint64
	executedCount  atomic.Uint64
	failedCount    atomic.Uint64
	workerCount    int
	queueCap       int
}

// DefaultImageProcessingConfig 默认配置
func DefaultImageProcessingConfig() *ImageProcessingConfig {
	return &ImageProcessingConfig{
		MaxConcurrentImages: 2,                 // 最多同时处理2张图片
		MaxImageSizeMB:      50,                // 50MB
		GCTriggerThreshold:  200 * 1024 * 1024, // 200MB
	}
}

// InitGlobalSemaphore 初始化全局信号量
func InitGlobalSemaphore(config *ImageProcessingConfig) {
	globalSemaphoreOnce.Do(func() {
		globalSemaphore = &ImageProcessingSemaphore{
			semaphore: make(chan struct{}, config.MaxConcurrentImages),
			config:    config,
		}
		workerPoolLog.Infof("Image processing semaphore initialized, max concurrent: %d", config.MaxConcurrentImages)
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
		workerPoolLog.Warnf("Releasing unacquired semaphore")
	}
}

// min 返回较小的整数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

func ShutdownGlobalPool(ctx context.Context) error {
	if globalPool == nil {
		return nil
	}
	return globalPool.ShutdownContext(ctx)
}

func CurrentInFlightTasks() []InFlightTaskSnapshot {
	return inFlightTasks.Snapshots()
}

// NewPool 创建新的任务池
func NewPool(workers, queueSize int) *Pool {
	if queueSize <= 0 {
		queueSize = 100
	}
	if workers <= 0 {
		workers = min(runtime.NumCPU(), 4)
	}

	p := &Pool{
		taskCh:   make(chan func(), queueSize),
		queueCap: queueSize,
		doneCh:   make(chan struct{}),
	}

	InitGlobalSemaphore(DefaultImageProcessingConfig())

	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	go func() {
		p.wg.Wait()
		close(p.doneCh)
	}()

	p.workerCount = workers
	workerPoolLog.Infof("Started with %d workers, queue size %d", workers, queueSize)
	return p
}

// worker 实际执行任务的 Goroutine
func (p *Pool) worker() {
	defer p.wg.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer vipsfile.ShutdownThread()

	for task := range p.taskCh {
		if task != nil {
			p.executeTaskWithRecovery(task)
			// Release libvips thread-local state between independent jobs.
			// This keeps long-lived worker threads from accumulating stale
			// per-thread state and avoids reusing corrupted state after cgo faults.
			vipsfile.ShutdownThread()
		}
	}
}

// executeTaskWithRecovery 包装单个任务
func (p *Pool) executeTaskWithRecovery(task func()) {
	p.executedCount.Add(1)
	defer func() {
		if r := recover(); r != nil {
			p.failedCount.Add(1)
			workerPoolLog.Errorf("Task panicked: %v", r)
		}
	}()
	task()
}

// Submit 提交异步任务到队列
func (p *Pool) Submit(task func()) (ok bool) {
	if p.isClosed.Load() {
		return false
	}
	if err := p.waitForMemory(); err != nil {
		workerPoolLog.Warnf("Rejecting task submission after backpressure wait: %v", err)
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
		workerPoolLog.Warnf("Task queue full, dropping task")
		return false
	}
}

var backpressureTimeout = 30 * time.Second
var backpressureInterval = 2 * time.Second

// waitForMemory waits up to 30s for memory to drop below the worker limit.
func (p *Pool) waitForMemory() error {
	// Fast path: memory is fine.
	if err := workerMemoryCheck(); err == nil {
		return nil
	}

	// Try to free memory immediately.
	runtime.GC()

	deadline := time.Now().Add(backpressureTimeout)
	for {
		if err := workerMemoryCheck(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("memory still over limit after %v backpressure wait", backpressureTimeout)
		}
		time.Sleep(backpressureInterval)
	}
}

// Stop 关闭池
func (p *Pool) Stop() {
	_ = p.ShutdownContext(context.Background())
}

func (p *Pool) ShutdownContext(ctx context.Context) error {
	if p.isClosed.CompareAndSwap(false, true) {
		workerPoolLog.Infof("Stopping")
		close(p.taskCh)
	}

	select {
	case <-p.doneCh:
		workerPoolLog.Infof("Stopped gracefully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

func beginInFlightTask(imageID uint, variantIDs []uint) *inFlightTaskLease {
	if imageID == 0 && len(variantIDs) == 0 {
		return nil
	}
	return inFlightTasks.Begin(imageID, variantIDs)
}

func (r *inFlightTaskRegistry) Begin(imageID uint, variantIDs []uint) *inFlightTaskLease {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID
	r.tasks[id] = InFlightTaskSnapshot{
		ImageID:    imageID,
		VariantIDs: append([]uint(nil), variantIDs...),
	}

	return &inFlightTaskLease{
		registry: r,
		id:       id,
	}
}

func (r *inFlightTaskRegistry) Snapshots() []InFlightTaskSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshots := make([]InFlightTaskSnapshot, 0, len(r.tasks))
	for _, snapshot := range r.tasks {
		snapshots = append(snapshots, InFlightTaskSnapshot{
			ImageID:    snapshot.ImageID,
			VariantIDs: append([]uint(nil), snapshot.VariantIDs...),
		})
	}
	return snapshots
}

func (l *inFlightTaskLease) Update(imageID uint, variantIDs []uint) {
	if l == nil || l.registry == nil {
		return
	}

	l.registry.mu.Lock()
	defer l.registry.mu.Unlock()

	if _, ok := l.registry.tasks[l.id]; !ok {
		return
	}
	l.registry.tasks[l.id] = InFlightTaskSnapshot{
		ImageID:    imageID,
		VariantIDs: append([]uint(nil), variantIDs...),
	}
}

func (l *inFlightTaskLease) Release() {
	if l == nil || l.registry == nil {
		return
	}

	l.registry.mu.Lock()
	defer l.registry.mu.Unlock()
	delete(l.registry.tasks, l.id)
}
