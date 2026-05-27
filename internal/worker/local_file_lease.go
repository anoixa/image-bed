package worker

import (
	"os"
	"sync/atomic"
)

const (
	localFileLeaseRequestOwned uint32 = iota
	localFileLeaseTransferred
	localFileLeaseConsumed
	localFileLeaseRemoved
)

// LocalFileLease tracks one temp file through request cleanup, queued
// conversion, and pipeline cleanup. It makes ownership transfer explicit
// instead of relying on a shared path string and comments.
type LocalFileLease struct {
	path  string
	state atomic.Uint32
}

func NewLocalFileLease(path string) *LocalFileLease {
	if path == "" {
		return nil
	}
	return &LocalFileLease{path: path}
}

func (l *LocalFileLease) Transfer() bool {
	if l == nil {
		return false
	}
	return l.state.CompareAndSwap(localFileLeaseRequestOwned, localFileLeaseTransferred)
}

func (l *LocalFileLease) CleanupIfRequestOwned() {
	if l == nil {
		return
	}
	if l.state.CompareAndSwap(localFileLeaseRequestOwned, localFileLeaseRemoved) {
		_ = os.Remove(l.path)
	}
}

func (l *LocalFileLease) CleanupTransferred() {
	if l == nil {
		return
	}
	if l.state.CompareAndSwap(localFileLeaseTransferred, localFileLeaseRemoved) {
		_ = os.Remove(l.path)
	}
}

func (l *LocalFileLease) Consume() (string, func(), bool) {
	if l == nil {
		return "", func() {}, false
	}
	if !l.state.CompareAndSwap(localFileLeaseTransferred, localFileLeaseConsumed) {
		return "", func() {}, false
	}
	return l.path, func() {
		if l.state.CompareAndSwap(localFileLeaseConsumed, localFileLeaseRemoved) {
			_ = os.Remove(l.path)
		}
	}, true
}
