package format

import (
	"testing"
)

func TestHumanReadableSize(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"bytes", 0, "0 B"},
		{"bytes small", 512, "512 B"},
		{"kilobytes", 1024, "1.00 KB"},
		{"megabytes", 1048576, "1.00 MB"},
		{"gigabytes", 1073741824, "1.00 GB"},
		{"terabytes", 1099511627776, "1.00 TB"},
		{"mixed", 1105197056, "1.03 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HumanReadableSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("HumanReadableSize(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestHumanReadableSizeWithPrecision(t *testing.T) {
	tests := []struct {
		name      string
		bytes     int64
		precision int
		expected  string
	}{
		{"zero precision", 1024, 0, "1 KB"},
		{"one precision", 1536, 1, "1.5 KB"},
		{"two precision", 1536, 2, "1.50 KB"},
		{"three precision", 1536, 3, "1.500 KB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HumanReadableSizeWithPrecision(tt.bytes, tt.precision)
			if result != tt.expected {
				t.Errorf("HumanReadableSizeWithPrecision(%d, %d) = %s, want %s",
					tt.bytes, tt.precision, result, tt.expected)
			}
		})
	}
}
