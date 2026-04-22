package image

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"sync/atomic"
)

const (
	requestOwnsTempFile uint32 = iota
	requestCleanupReleased
	requestCleanupCompleted
)

type tempFileHandle struct {
	path  string
	state atomic.Uint32
}

func newTempFileHandle(path string) *tempFileHandle {
	return &tempFileHandle{path: path}
}

func (h *tempFileHandle) releaseRequestCleanup() {
	if h == nil {
		return
	}
	h.state.CompareAndSwap(requestOwnsTempFile, requestCleanupReleased)
}

func (h *tempFileHandle) cleanupIfRequestOwned() {
	if h == nil {
		return
	}
	if h.state.CompareAndSwap(requestOwnsTempFile, requestCleanupCompleted) {
		_ = os.Remove(h.path)
	}
}

// UploadSource describes a single uploaded file that can be reopened for
// validation, hashing, image inspection, and final storage save.
type UploadSource struct {
	FileName        string
	FileSize        int64
	TempFilePath    string // optional: local temp file path (ownership transferred, caller must NOT clean up)
	PrecomputedHash string // optional: SHA256 hash computed during initial write
	tempFileHandle  *tempFileHandle
	Open            func() (io.ReadSeekCloser, error)
}

func uploadSourceFromFileHeader(fileHeader *multipart.FileHeader) UploadSource {
	return UploadSource{
		FileName: fileHeader.Filename,
		FileSize: fileHeader.Size,
		Open: func() (io.ReadSeekCloser, error) {
			file, err := fileHeader.Open()
			if err != nil {
				return nil, err
			}
			return file, nil
		},
	}
}

// NewTempUploadSource returns an UploadSource backed by a temp file path.
func NewTempUploadSource(fileName, tempPath string, fileSize int64) UploadSource {
	handle := newTempFileHandle(tempPath)
	return UploadSource{
		FileName:       fileName,
		FileSize:       fileSize,
		TempFilePath:   tempPath,
		tempFileHandle: handle,
		Open: func() (io.ReadSeekCloser, error) {
			file, err := os.Open(tempPath)
			if err != nil {
				return nil, fmt.Errorf("open temp upload %q: %w", tempPath, err)
			}
			return file, nil
		},
	}
}

// ReleaseRequestCleanup disarms request-scoped cleanup after ownership of the
// temp file has been safely transferred to the converter/pipeline chain.
func (s UploadSource) ReleaseRequestCleanup() {
	if s.tempFileHandle != nil {
		s.tempFileHandle.releaseRequestCleanup()
	}
}

// CleanupRequestTempFile removes the temp file only if the request still owns
// its lifecycle. Once released, cleanup becomes a no-op.
func (s UploadSource) CleanupRequestTempFile() {
	if s.tempFileHandle != nil {
		s.tempFileHandle.cleanupIfRequestOwned()
		return
	}
	if s.TempFilePath != "" {
		_ = os.Remove(s.TempFilePath)
	}
}
