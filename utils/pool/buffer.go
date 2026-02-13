package pool

import "sync"

// SharedBufferPool 共享的 32KB 缓冲区池
var SharedBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024)
	},
}
