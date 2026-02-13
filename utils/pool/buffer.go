package pool

import "sync"

// BufferSize 统一缓冲区大小（256KB）
// 权衡内存使用和 I/O 效率，适用于大多数文件传输场景
const BufferSize = 256 * 1024

// SharedBufferPool 共享缓冲区池
var SharedBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, BufferSize)
	},
}
