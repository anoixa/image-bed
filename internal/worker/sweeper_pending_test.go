package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSweeperRetriggersDuePending(t *testing.T) {
	db := setupSweeperTestDB(t)
	vRepo := images.NewVariantRepository(db)
	iRepo := images.NewRepository(db)

	require.NoError(t, db.Create(&models.Image{
		ID: 1, Identifier: "a", StoragePath: "p", OriginalName: "a.jpg",
		FileSize: 1024, MimeType: "image/jpeg", StorageConfigID: 1, FileHash: "h1",
	}).Error)
	v, err := vRepo.UpsertPending(1, models.FormatWebP)
	require.NoError(t, err)
	// make it due (past backoff, as a paused-due-to-disabled variant would be)
	require.NoError(t, vRepo.SetPendingBackoff([]uint{v.ID}, time.Now().Add(-time.Minute)))

	var triggered atomic.Int32
	triggerFn := func(img *models.Image) { triggered.Add(1) }

	sweepOnce(context.Background(), vRepo, iRepo, triggerFn)
	assert.Equal(t, int32(1), triggered.Load(), "sweeper must re-trigger the due-pending image")
}

func TestSweeperSkipsFutureBackoffPending(t *testing.T) {
	db := setupSweeperTestDB(t)
	vRepo := images.NewVariantRepository(db)
	iRepo := images.NewRepository(db)

	require.NoError(t, db.Create(&models.Image{
		ID: 1, Identifier: "a", StoragePath: "p", OriginalName: "a.jpg",
		FileSize: 1024, MimeType: "image/jpeg", StorageConfigID: 1, FileHash: "h1",
	}).Error)
	v, err := vRepo.UpsertPending(1, models.FormatWebP)
	require.NoError(t, err)
	// future backoff -> not due this sweep
	require.NoError(t, vRepo.SetPendingBackoff([]uint{v.ID}, time.Now().Add(time.Hour)))

	var triggered atomic.Int32
	triggerFn := func(img *models.Image) { triggered.Add(1) }

	sweepOnce(context.Background(), vRepo, iRepo, triggerFn)
	assert.Equal(t, int32(0), triggered.Load(), "future-backoff pending must not be re-triggered")
}

func TestExecuteSkipsWhenStorageDisabled(t *testing.T) {
	storage.ResetForTest()
	t.Cleanup(storage.ResetForTest)
	require.NoError(t, storage.AddOrUpdateProvider(storage.StorageConfig{
		ID: 5, Name: "off", Type: "local", LocalPath: t.TempDir(), IsEnabled: false,
	}))

	variantRepo := &mockVariantRepo{}
	imageRepo := &mockImageRepo{}
	task := &ImagePipelineTask{
		ImageID:         1,
		ImageIdentifier: "disabled-test",
		WebPVariantID:   9,
		StorageConfigID: 5, // explicitly disabled
		VariantRepo:     variantRepo,
		ImageRepo:       imageRepo,
	}
	task.Execute()

	// disabled storage -> Execute skips before any CAS or failure marking
	assert.Empty(t, variantRepo.statusCASCalls, "no CAS should be attempted for disabled storage")
	assert.Empty(t, variantRepo.updateFailedCalls, "no failure marking for disabled storage")
}
