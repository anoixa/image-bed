package utils

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsContextCanceled(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "direct context.Canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "wrapped context.Canceled",
			err:      fmt.Errorf("wrapped: %w", context.Canceled),
			expected: true,
		},
		{
			name:     "string contains context canceled",
			err:      errors.New("Head \"http://example.com\": context canceled"),
			expected: true,
		},
		{
			name:     "MinIO wrapped error",
			err:      errors.New("failed to stream object 'path': Head \"http://192.168.10.3:9000/bucket/path\": context canceled"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "context.DeadlineExceeded (not canceled)",
			err:      context.DeadlineExceeded,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsContextCanceled(tt.err)
			if result != tt.expected {
				t.Errorf("IsContextCanceled(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsClientDisconnect(t *testing.T) {
	// IsClientDisconnect 是 IsContextCanceled 的别名
	if IsClientDisconnect(context.Canceled) != true {
		t.Error("IsClientDisconnect should return true for context.Canceled")
	}
	if IsClientDisconnect(errors.New("other error")) != false {
		t.Error("IsClientDisconnect should return false for other errors")
	}
}
