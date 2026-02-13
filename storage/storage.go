package storage

import (
	"io"
)

// ImageStream 图片流结构
type ImageStream struct {
	Reader      io.ReadSeeker
	ContentType string
	Size        int64
}
