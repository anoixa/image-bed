package image

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
)

// UploadSource describes a single uploaded file that can be reopened for
// validation, hashing, image inspection, and final storage save.
type UploadSource struct {
	FileName        string
	FileSize        int64
	TempFilePath    string // optional: local temp file path (ownership transferred, caller must NOT clean up)
	PrecomputedHash string // optional: SHA256 hash computed during initial write
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
	return UploadSource{
		FileName: fileName,
		FileSize: fileSize,
		Open: func() (io.ReadSeekCloser, error) {
			file, err := os.Open(tempPath)
			if err != nil {
				return nil, fmt.Errorf("open temp upload %q: %w", tempPath, err)
			}
			return file, nil
		},
	}
}
