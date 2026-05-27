package image

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"

	"github.com/anoixa/image-bed/internal/worker"
)

// UploadSource describes a single uploaded file that can be reopened for
// validation, hashing, image inspection, and final storage save.
type UploadSource struct {
	FileName        string
	FileSize        int64
	PrecomputedHash string // optional: SHA256 hash computed during initial write
	tempFilePath    string
	tempFileLease   *worker.LocalFileLease
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
	lease := worker.NewLocalFileLease(tempPath)
	return UploadSource{
		FileName:      fileName,
		FileSize:      fileSize,
		tempFilePath:  tempPath,
		tempFileLease: lease,
		Open: func() (io.ReadSeekCloser, error) {
			file, err := os.Open(tempPath)
			if err != nil {
				return nil, fmt.Errorf("open temp upload %q: %w", tempPath, err)
			}
			return file, nil
		},
	}
}

func (s UploadSource) TransferTempFile() *worker.LocalFileLease {
	if s.tempFileLease == nil {
		return nil
	}
	if !s.tempFileLease.Transfer() {
		return nil
	}
	return s.tempFileLease
}

// CleanupRequestTempFile removes the temp file only if the request still owns
// its lifecycle. Once released, cleanup becomes a no-op.
func (s UploadSource) CleanupRequestTempFile() {
	if s.tempFileLease != nil {
		s.tempFileLease.CleanupIfRequestOwned()
		return
	}
	if s.tempFilePath != "" {
		_ = os.Remove(s.tempFilePath)
	}
}
