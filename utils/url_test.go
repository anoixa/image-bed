package utils

import "testing"

func TestExtractCookieDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "full URL with protocol and port",
			input:    "http://localhost:8081",
			expected: "localhost",
		},
		{
			name:     "HTTPS URL with port",
			input:    "https://localhost:8081",
			expected: "localhost",
		},
		{
			name:     "domain with port only",
			input:    "localhost:8081",
			expected: "localhost",
		},
		{
			name:     "domain only",
			input:    "localhost",
			expected: "localhost",
		},
		{
			name:     "full domain with protocol",
			input:    "https://example.com",
			expected: "example.com",
		},
		{
			name:     "full domain with protocol and port",
			input:    "https://example.com:443",
			expected: "example.com",
		},
		{
			name:     "subdomain",
			input:    "https://api.example.com",
			expected: "api.example.com",
		},
		{
			name:     "IP address with port",
			input:    "http://192.168.1.1:8080",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 address",
			input:    "http://[::1]:8080",
			expected: "::1",
		},
		{
			name:     "URL with path",
			input:    "https://example.com/api/v1",
			expected: "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractCookieDomain(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractCookieDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
