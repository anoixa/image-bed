package images

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMultipartUploadRequestStreamsFilesAndFields(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	require.NoError(t, writer.WriteField("is_public", "false"))
	fileWriter, err := writer.CreateFormFile("files", "a.txt")
	require.NoError(t, err)
	_, err = io.Copy(fileWriter, bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("strategy_id", "12"))
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, "/upload", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	settings := &dbconfig.ImageProcessingSettings{
		MaxFileSizeMB:   10,
		MaxBatchTotalMB: 20,
	}

	parsed, cleanup, err := parseMultipartUploadRequest(req, settings)
	require.NoError(t, err)
	defer cleanup()

	require.Len(t, parsed.files, 1)
	assert.Equal(t, "12", parsed.strategyID)
	assert.Equal(t, "false", parsed.visibility)

	file, err := parsed.files[0].Open()
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestParseMultipartUploadRequestRejectsTooManyFiles(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for i := 0; i < 11; i++ {
		fileWriter, err := writer.CreateFormFile("files", "f.txt")
		require.NoError(t, err)
		_, err = io.Copy(fileWriter, bytes.NewReader([]byte("a")))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, "/upload", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, cleanup, err := parseMultipartUploadRequest(req, &dbconfig.ImageProcessingSettings{
		MaxFileSizeMB:   10,
		MaxBatchTotalMB: 20,
	})
	if cleanup != nil {
		defer cleanup()
	}

	require.Error(t, err)
	requestErr, ok := err.(*uploadRequestError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, requestErr.status)
	assert.Equal(t, "Maximum 10 files allowed per upload", requestErr.message)
}

func TestParseMultipartUploadRequestCleanupRemovesUnreleasedTempFile(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	fileWriter, err := writer.CreateFormFile("files", "a.txt")
	require.NoError(t, err)
	_, err = io.Copy(fileWriter, bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, "/upload", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	parsed, cleanup, err := parseMultipartUploadRequest(req, &dbconfig.ImageProcessingSettings{
		MaxFileSizeMB:   10,
		MaxBatchTotalMB: 20,
	})
	require.NoError(t, err)
	require.Len(t, parsed.files, 1)

	path := parsed.files[0].TempFilePath
	_, err = os.Stat(path)
	require.NoError(t, err)

	cleanup()

	_, err = os.Stat(path)
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestParseMultipartUploadRequestCleanupSkipsReleasedTempFile(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	fileWriter, err := writer.CreateFormFile("files", "a.txt")
	require.NoError(t, err)
	_, err = io.Copy(fileWriter, bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, "/upload", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	parsed, cleanup, err := parseMultipartUploadRequest(req, &dbconfig.ImageProcessingSettings{
		MaxFileSizeMB:   10,
		MaxBatchTotalMB: 20,
	})
	require.NoError(t, err)
	require.Len(t, parsed.files, 1)

	path := parsed.files[0].TempFilePath
	parsed.files[0].ReleaseRequestCleanup()
	cleanup()

	_, err = os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
}
