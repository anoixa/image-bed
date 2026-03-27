package worker

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/anoixa/image-bed/utils"
	"github.com/stretchr/testify/assert"
)

func TestSubmitRejectsTaskWhenMemoryLimitExceeded(t *testing.T) {
	previousCheck := workerMemoryCheck
	workerMemoryCheck = func() error {
		return errors.New("memory limit exceeded")
	}
	defer func() {
		workerMemoryCheck = previousCheck
	}()

	pool := NewPool(1, 1)
	defer pool.Stop()

	var executed atomic.Bool
	ok := pool.Submit(func() {
		executed.Store(true)
	})

	assert.False(t, ok)
	assert.False(t, executed.Load())
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
