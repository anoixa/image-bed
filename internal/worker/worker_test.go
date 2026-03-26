package worker

import (
	"errors"
	"sync/atomic"
	"testing"

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
