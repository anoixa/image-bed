package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anoixa/image-bed/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitDoesNotWaitForMemoryBackpressure(t *testing.T) {
	callCount := 0
	previousCheck := workerMemoryCheck
	workerMemoryCheck = func() error {
		callCount++
		return errors.New("memory limit exceeded")
	}
	defer func() {
		workerMemoryCheck = previousCheck
	}()

	// Shorten backpressure timeout for test.
	origTimeout := backpressureTimeout
	origInterval := backpressureInterval
	backpressureTimeout = 100 * time.Millisecond
	backpressureInterval = 50 * time.Millisecond
	defer func() {
		backpressureTimeout = origTimeout
		backpressureInterval = origInterval
	}()

	pool := NewPool(1, 1)
	defer pool.Stop()

	executed := make(chan struct{})
	ok := pool.Submit(func() {
		close(executed)
	})

	require.True(t, ok)
	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("submitted task was not executed")
	}
	assert.Equal(t, 0, callCount, "Submit must not run memory checks")
}

func TestEffectiveWorkerMemoryMB(t *testing.T) {
	assert.Equal(t, 72.0, effectiveWorkerMemoryMB(utils.MemoryStats{
		RSSMB:       128,
		HeapAllocMB: 64,
		HeapInUseMB: 60,
		VipsMemMB:   8,
	}))

	assert.Equal(t, 180.0, effectiveWorkerMemoryMB(utils.MemoryStats{
		RSSMB:       90,
		HeapAllocMB: 140,
		HeapInUseMB: 135,
		VipsMemMB:   40,
	}))

	assert.Equal(t, 96.0, effectiveWorkerMemoryMB(utils.MemoryStats{
		RSSMB:       620,
		HeapAllocMB: 14,
		HeapInUseMB: 17,
		VipsMemMB:   79,
	}))
}

func TestShutdownContextTimesOutWhenWorkerIsStillRunning(t *testing.T) {
	pool := NewPool(1, 1)

	blocker := make(chan struct{})
	submitted := pool.Submit(func() {
		<-blocker
	})
	require.True(t, submitted)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := pool.ShutdownContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	close(blocker)
	require.NoError(t, pool.ShutdownContext(context.Background()))
}

func TestInFlightTaskLeaseTracksLatestVariantSet(t *testing.T) {
	lease := beginInFlightTask(42, []uint{1, 2, 3})
	require.NotNil(t, lease)
	defer lease.Release()

	lease.Update(42, []uint{2, 3})
	snapshots := CurrentInFlightTasks()
	require.Len(t, snapshots, 1)
	assert.Equal(t, uint(42), snapshots[0].ImageID)
	assert.Equal(t, []uint{2, 3}, snapshots[0].VariantIDs)

	lease.Release()
	assert.Empty(t, CurrentInFlightTasks())
}
