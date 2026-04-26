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
	touchCalls         [][]uint
	statusCASCalls     []statusCASCall
}

type statusCASCall struct {
	id        uint
	expected  string
	newStatus string
	errMsg    string
}

func (m *mockVariantRepo) UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error) {
	m.statusCASCalls = append(m.statusCASCalls, statusCASCall{
		id:        id,
		expected:  expected,
		newStatus: newStatus,
		errMsg:    errMsg,
	})
	return true, nil
}

func (m *mockVariantRepo) UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, fileHash string, width, height int) error {
	return m.updateCompletedErr
}

func (m *mockVariantRepo) UpdateFailed(id uint, errMsg string) error {
	m.updateFailedCalls = append(m.updateFailedCalls, id)
	return nil
}

func (m *mockVariantRepo) TouchProcessing(ids []uint) error {
	copied := append([]uint(nil), ids...)
	m.touchCalls = append(m.touchCalls, copied)
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

type mockImageRepo struct {
	touchedImageIDs []uint
	statusUpdates   []imageStatusUpdate
}

type imageStatusUpdate struct {
	imageID uint
	status  models.ImageVariantStatus
}

func (m *mockImageRepo) UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error {
	m.statusUpdates = append(m.statusUpdates, imageStatusUpdate{imageID: imageID, status: status})
	return nil
}

func (m *mockImageRepo) TouchVariantProcessingStatus(imageID uint) error {
	m.touchedImageIDs = append(m.touchedImageIDs, imageID)
	return nil
}

func (m *mockImageRepo) GetImageByID(id uint) (*models.Image, error) {
	return nil, nil
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
	acquiredVariants := []uint{7}

	err := task.saveVariantResults(&acquiredVariants, nil, result, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
	assert.Contains(t, repo.updateFailedCalls, uint(7), "UpdateFailed must be called for the variant")
}

func TestTouchProcessingStateRefreshesImageAndVariants(t *testing.T) {
	variantRepo := &mockVariantRepo{}
	imageRepo := &mockImageRepo{}
	task := &ImagePipelineTask{
		ImageID:     42,
		VariantRepo: variantRepo,
		ImageRepo:   imageRepo,
	}

	task.touchProcessingState([]uint{3, 5})

	require.Len(t, variantRepo.touchCalls, 1)
	assert.Equal(t, []uint{3, 5}, variantRepo.touchCalls[0])
	assert.Equal(t, []uint{42}, imageRepo.touchedImageIDs)
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

func TestFinalizeOnlyRollsBackStillTrackedVariants(t *testing.T) {
	repo := &mockVariantRepo{}
	task := &ImagePipelineTask{VariantRepo: repo}

	acquiredVariants := []uint{11, 12}
	task.releaseTrackedVariant(&acquiredVariants, 11)
	task.finalize(&acquiredVariants)

	require.Len(t, repo.statusCASCalls, 1)
	assert.Equal(t, statusCASCall{
		id:        12,
		expected:  models.VariantStatusProcessing,
		newStatus: models.VariantStatusPending,
		errMsg:    "",
	}, repo.statusCASCalls[0])
}

func TestExecuteMemoryBackpressureFailureAfterSemaphore(t *testing.T) {
	previousCheck := workerMemoryCheck
	workerMemoryCheck = func() error {
		return errors.New("memory limit exceeded")
	}
	defer func() {
		workerMemoryCheck = previousCheck
	}()

	origTimeout := backpressureTimeout
	origInterval := backpressureInterval
	backpressureTimeout = 20 * time.Millisecond
	backpressureInterval = 5 * time.Millisecond
	defer func() {
		backpressureTimeout = origTimeout
		backpressureInterval = origInterval
	}()

	variantRepo := &mockVariantRepo{}
	imageRepo := &mockImageRepo{}
	task := &ImagePipelineTask{
		ImageID:         42,
		ImageIdentifier: "memory-test",
		WebPVariantID:   7,
		VariantRepo:     variantRepo,
		ImageRepo:       imageRepo,
	}

	task.Execute()

	require.Contains(t, variantRepo.updateFailedCalls, uint(7))
	require.Contains(t, imageRepo.statusUpdates, imageStatusUpdate{
		imageID: 42,
		status:  models.ImageVariantStatusFailed,
	})
	assert.Empty(t, CurrentInFlightTasks())

	for _, call := range variantRepo.statusCASCalls {
		assert.False(t, call.expected == models.VariantStatusProcessing && call.newStatus == models.VariantStatusPending, "memory failure must not roll variant back to pending")
	}
}
