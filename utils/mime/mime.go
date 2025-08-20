package mime

import (
	"fmt"
	"io"
	"net/http"
)

func SniffContentType(stream io.ReadSeeker) (string, error) {
	buffer := make([]byte, 512)

	n, err := stream.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read stream for mime sniffing: %w", err)
	}

	contentType := http.DetectContentType(buffer[:n])

	_, err = stream.Seek(0, io.SeekStart)
	if err != nil {
		return "", fmt.Errorf("failed to seek stream back to start after sniffing: %w", err)
	}

	return contentType, nil
}
