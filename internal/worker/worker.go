package worker

import (
	"log"
	"runtime"
	"sync"
	"time"
)

// Task 异步任务接口
type Task interface {
	Execute()
}

var (
	taskCh   chan Task
	once     sync.Once
	stopOnce sync.Once
	stopCh   chan struct{}
)

// InitGlobalPool 初始化全局任务队列
func InitGlobalPool(workers, queueSize int) {
	once.Do(func() {
		if queueSize <= 0 {
			queueSize = 1000
		}
		if workers <= 0 {
			workers = runtime.NumCPU() * 2
		}

		taskCh = make(chan Task, queueSize)
		stopCh = make(chan struct{})

		// 启动固定数量的 worker goroutine
		for i := 0; i < workers; i++ {
			go worker()
		}

		log.Printf("Worker pool started with %d workers", workers)
	})
}

// worker 工作 goroutine
func worker() {
	for {
		select {
		case task := <-taskCh:
			if task != nil {
				task.Execute()
			}
		case <-stopCh:
			return
		}
	}
}

// Submit 提交任务到队列
func Submit(task Task) {
	if taskCh == nil {
		// 如果未初始化，直接同步执行
		task.Execute()
		return
	}
	select {
	case taskCh <- task:
	default:
		// 队列满时，直接异步执行
		go task.Execute()
	}
}

// StopGlobalPool 停止全局任务队列
func StopGlobalPool() {
	stopOnce.Do(func() {
		if stopCh != nil {
			close(stopCh)
		}
		log.Println("Worker pool stopped")
	})
}

// TrySubmit 带重试的任务提交
// 尝试提交任务到队列，如果失败则重试指定次数
func TrySubmit(task Task, retries int, delay time.Duration) bool {
	if taskCh == nil {
		task.Execute()
		return true
	}

	for i := 0; i < retries; i++ {
		select {
		case taskCh <- task:
			return true
		default:
			if i < retries-1 {
				time.Sleep(delay)
			}
		}
	}

	// 重试都失败后，异步执行
	go task.Execute()
	return true
}

// SubmitBlocking 阻塞式提交任务，带超时
// 返回 true 表示成功提交，false 表示超时
func SubmitBlocking(task Task, timeout time.Duration) bool {
	if taskCh == nil {
		task.Execute()
		return true
	}

	done := make(chan struct{})
	go func() {
		taskCh <- task
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
