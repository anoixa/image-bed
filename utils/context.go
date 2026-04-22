package utils

import (
	"context"
	"errors"
	"strings"
	"time"
)

// DetachedContext returns a fresh background-based context for best-effort work
// that should survive request cancellation, while still respecting a hard timeout.
func DetachedContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

// IsContextCanceled reports whether err represents request cancellation.
func IsContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}

	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "context canceled")
}

// IsClientDisconnect reports whether the client side terminated the request.
func IsClientDisconnect(err error) bool {
	return IsContextCanceled(err)
}
