package validator

import (
	"io"
	"net/http"
)

// allowedImageMimeTypes Allowed image types
var allowedImageMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

// IsImage Verify if the file content isan allowed image type.
func IsImage(file io.ReadSeeker) (bool, error) {
	buffer := make([]byte, 512)
	_, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, err
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return false, err
	}

	// 检测 MIME 类型
	mimeType := http.DetectContentType(buffer)

	if _, ok := allowedImageMimeTypes[mimeType]; ok {
		return true, nil
	}

	return false, nil
}
