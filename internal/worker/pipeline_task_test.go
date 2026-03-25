package worker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVariantRepo is a test double for VariantRepository.
type mockVariantRepo struct {
	updateCompletedErr error
	updateFailedCalls  []uint
}

func (m *mockVariantRepo) UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error) {
	return true, nil
}

func (m *mockVariantRepo) UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, fileHash string, width, height int) error {
	return m.updateCompletedErr
}

func (m *mockVariantRepo) UpdateFailed(id uint, errMsg string, _ bool) error {
	m.updateFailedCalls = append(m.updateFailedCalls, id)
	return nil
}

func (m *mockVariantRepo) GetByID(id uint) (*models.ImageVariant, error) {
	return nil, nil
}

func (m *mockVariantRepo) DeleteVariant(id uint) error {
	return nil
}

func TestGetProcessingFilePath_LocalStorage(t *testing.T) {
	dir := t.TempDir()
	ls, err := storage.NewLocalStorage(dir)
	require.NoError(t, err)

	relPath := "img/test.jpg"
	full := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte("fakejpeg"), 0600))

	task := &ImagePipelineTask{
		StoragePath: relPath,
		Storage:     ls,
	}

	path, cleanup, err := task.getProcessingFilePath(context.Background())
	require.NoError(t, err)

	assert.Equal(t, full, path)

	// call cleanup explicitly first, then verify local file is untouched
	cleanup()
	_, statErr := os.Stat(full)
	assert.NoError(t, statErr, "local storage file must not be deleted by cleanup")
}

func TestSaveVariantResults_UpdateCompletedError_CallsUpdateFailed(t *testing.T) {
	repo := &mockVariantRepo{updateCompletedErr: errors.New("db down")}
	task := &ImagePipelineTask{
		WebPVariantID: 7,
		VariantRepo:   repo,
	}
	result := &pipelineResult{StoragePath: "webp/foo.webp", Width: 100, Height: 100, FileSize: 1000, FileHash: "abc"}

	err := task.saveVariantResults(nil, result)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
	assert.Contains(t, repo.updateFailedCalls, uint(7), "UpdateFailed must be called for the variant")
}

func TestWriteVariantBufferToTempFile(t *testing.T) {
	data := []byte("variant-payload")

	file, size, hashValue, cleanup, err := writeVariantBufferToTempFile(data)
	require.NoError(t, err)
	defer cleanup()

	assert.IsType(t, &os.File{}, file)
	assert.Equal(t, int64(len(data)), size)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), hashValue)

	readBack, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, data, readBack)
}

func TestNewSequentialImportParams(t *testing.T) {
	params := newSequentialImportParams()

	require.NotNil(t, params)
	assert.True(t, params.FailOnError.IsSet())
	assert.True(t, params.FailOnError.Get())
	assert.True(t, params.Access.IsSet())
	assert.Equal(t, vips.AccessSequential, params.Access.Get())
}
