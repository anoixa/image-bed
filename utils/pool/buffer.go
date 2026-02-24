package pool

import "sync"

// BufferSize 统一缓冲区大小（256KB）
const BufferSize = 256 * 1024

// SharedBufferPool 共享缓冲区池
var SharedBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, BufferSize)
		return &buf
	},
}
