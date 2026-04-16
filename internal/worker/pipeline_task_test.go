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
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
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

func (m *mockVariantRepo) UpdateFailed(id uint, errMsg string) error {
	m.updateFailedCalls = append(m.updateFailedCalls, id)
	return nil
}

func (m *mockVariantRepo) GetByID(id uint) (*models.ImageVariant, error) {
	return nil, nil
}

func (m *mockVariantRepo) DeleteVariant(id uint) error {
	return nil
}

func (m *mockVariantRepo) ResetStaleProcessing(olderThan time.Duration) (int64, error) {
	return 0, nil
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

	err := task.saveVariantResults(nil, result, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
	assert.Contains(t, repo.updateFailedCalls, uint(7), "UpdateFailed must be called for the variant")
}

func TestStageVariantFileFromPath(t *testing.T) {
	data := []byte("variant-payload")
	path := filepath.Join(t.TempDir(), "variant.webp")
	require.NoError(t, os.WriteFile(path, data, 0600))

	file, size, hashValue, cleanup, err := stageVariantFileFromPath(path)
	require.NoError(t, err)
	defer cleanup()

	assert.IsType(t, &os.File{}, file)
	assert.Equal(t, int64(len(data)), size)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), hashValue)

	readBack, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, data, readBack)
}

func TestCreateVariantTempPath(t *testing.T) {
	path, cleanup, err := createVariantTempPath()
	require.NoError(t, err)
	defer cleanup()

	assert.FileExists(t, path)
}

func TestResolveImageVariantStatus(t *testing.T) {
	t.Run("thumbnail completed and webp skipped", func(t *testing.T) {
		status := resolveImageVariantStatus(true, true, false, true, false, false, false, true, false)
		assert.Equal(t, models.ImageVariantStatusThumbnailCompleted, status)
	})

	t.Run("both completed", func(t *testing.T) {
		status := resolveImageVariantStatus(true, true, false, true, true, false, false, false, false)
		assert.Equal(t, models.ImageVariantStatusCompleted, status)
	})

	t.Run("webp only completed", func(t *testing.T) {
		status := resolveImageVariantStatus(false, true, false, false, true, false, false, false, false)
		assert.Equal(t, models.ImageVariantStatusCompleted, status)
	})

	t.Run("all skipped", func(t *testing.T) {
		status := resolveImageVariantStatus(true, true, false, false, false, false, true, true, false)
		assert.Equal(t, models.ImageVariantStatusNone, status)
	})

	t.Run("avif only completed", func(t *testing.T) {
		status := resolveImageVariantStatus(false, false, true, false, false, true, false, false, false)
		assert.Equal(t, models.ImageVariantStatusCompleted, status)
	})
}

func TestShouldKeepAVIF(t *testing.T) {
	assert.True(t, shouldKeepAVIF(94, 100))
	assert.False(t, shouldKeepAVIF(95, 100))
	assert.False(t, shouldKeepAVIF(110, 100))
	assert.True(t, shouldKeepAVIF(90, 0))
}
