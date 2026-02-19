package worker

import (
	"log"
	"runtime"
	"sync"
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
