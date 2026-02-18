package worker

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"
)

// Task 异步任务接口
type Task interface {
	Execute()
}

// WorkerPool 协程池
type WorkerPool struct {
	workers int
	queue   chan Task
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	mu      sync.Mutex
}

var (
	globalPool *WorkerPool
	once       sync.Once
)

// InitGlobalPool 初始化全局协程池
func InitGlobalPool(workers, queueSize int) {
	once.Do(func() {
		if workers <= 0 {
			workers = 12 // 默认12个worker
		}
		globalPool = NewWorkerPool(workers, queueSize)
		globalPool.Start()
	})
}

// GetGlobalPool 获取全局协程池
func GetGlobalPool() *WorkerPool {
	return globalPool
}

// StopGlobalPool 停止全局协程池
func StopGlobalPool() {
	if globalPool != nil {
		globalPool.Stop()
	}
}

// NewWorkerPool 创建工作池
func NewWorkerPool(workers, queueSize int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	if queueSize <= 0 {
		queueSize = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		workers: workers,
		queue:   make(chan Task, queueSize),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 启动工作池
func (p *WorkerPool) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	p.started = true
	log.Printf("Async worker pool started with %d workers", p.workers)
}

// Stop 停止工作池
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()
	close(p.queue)

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()

	log.Println("Async worker pool stopped")
}

// Submit 提交任务（非阻塞，队列满时丢弃）
func (p *WorkerPool) Submit(task Task) bool {
	select {
	case p.queue <- task:
		return true
	case <-p.ctx.Done():
		return false
	default:
		// 队列满时，直接丢弃任务但记录日志
		log.Println("WARN: Worker pool queue is full, task dropped")
		return false
	}
}

// SubmitBlocking 阻塞提交任务，队列满时等待（带超时）
func (p *WorkerPool) SubmitBlocking(task Task, timeout time.Duration) bool {
	if timeout <= 0 {
		// 无限期等待
		select {
		case p.queue <- task:
			return true
		case <-p.ctx.Done():
			return false
		}
	}

	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	select {
	case p.queue <- task:
		return true
	case <-ctx.Done():
		return false
	}
}

// TrySubmit 尝试提交任务，可配置重试次数和间隔
func (p *WorkerPool) TrySubmit(task Task, retries int, interval time.Duration) bool {
	for i := 0; i <= retries; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		if p.Submit(task) {
			return true
		}
	}
	return false
}

// worker 工作协程
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case task, ok := <-p.queue:
			if !ok {
				return
			}
			p.executeTask(task)
		case <-p.ctx.Done():
			return
		}
	}
}

// executeTask 执行任务并捕获 panic
func (p *WorkerPool) executeTask(task Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic recovered in async task: %v", r)
		}
	}()
	task.Execute()
}

// Submit 提交任务到全局池
func Submit(task Task) bool {
	if globalPool == nil {
		InitGlobalPool(12, 1000)
	}
	return globalPool.Submit(task)
}

// TrySubmit 尝试提交任务到全局池，可配置重试次数和间隔
func TrySubmit(task Task, retries int, interval time.Duration) bool {
	if globalPool == nil {
		InitGlobalPool(12, 1000)
	}
	return globalPool.TrySubmit(task, retries, interval)
}
