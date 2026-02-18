package format

import (
	"testing"
)

func TestNegotiator_Negotiate(t *testing.T) {
	tests := []struct {
		name         string
		acceptHeader string
		enabled      []string
		available    map[FormatType]bool
		want         FormatType
	}{
		{
			name:         "Chrome supports AVIF+WebP, both available - should return AVIF",
			acceptHeader: "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
			enabled:      []string{"avif", "webp"},
			available:    map[FormatType]bool{FormatAVIF: true, FormatWebP: true},
			want:         FormatAVIF,
		},
		{
			name:         "Chrome supports AVIF+WebP, only WebP available - should return WebP",
			acceptHeader: "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
			enabled:      []string{"avif", "webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatWebP,
		},
		{
			name:         "Safari only supports WebP - should return WebP",
			acceptHeader: "image/webp,*/*",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatWebP,
		},
		{
			name:         "Old browser accepts any format - should return WebP",
			acceptHeader: "*/*",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatWebP,
		},
		{
			name:         "Client rejects WebP (q=0) - should return original",
			acceptHeader: "image/webp;q=0,*/*",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatOriginal,
		},
		{
			name:         "WebP variant not generated yet - should return original",
			acceptHeader: "image/webp,*/*",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{},
			want:         FormatOriginal,
		},
		{
			name:         "WebP disabled in config - should return original",
			acceptHeader: "image/webp,*/*",
			enabled:      []string{},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatOriginal,
		},
		{
			name:         "Empty Accept header - should return original",
			acceptHeader: "",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatOriginal,
		},
		{
			name:         "Firefox with AVIF support - should return AVIF",
			acceptHeader: "image/avif,image/webp,*/*",
			enabled:      []string{"avif", "webp"},
			available:    map[FormatType]bool{FormatAVIF: true, FormatWebP: true},
			want:         FormatAVIF,
		},
		{
			name:         "WebP with lower quality preference - still return WebP",
			acceptHeader: "image/webp;q=0.5,*/*;q=0.8",
			enabled:      []string{"webp"},
			available:    map[FormatType]bool{FormatWebP: true},
			want:         FormatWebP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNegotiator(tt.enabled)
			got := n.Negotiate(tt.acceptHeader, tt.available)
			if got != tt.want {
				t.Errorf("Negotiate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAcceptHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int // expected number of preferences
	}{
		{
			name:   "Chrome header",
			header: "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
			want:   4,
		},
		{
			name:   "Safari header",
			header: "image/webp,*/*",
			want:   2,
		},
		{
			name:   "Empty header",
			header: "",
			want:   0,
		},
		{
			name:   "With q values",
			header: "image/webp;q=0.9,image/avif;q=0.8,*/*;q=0.5",
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAcceptHeader(tt.header)
			if len(got) != tt.want {
				t.Errorf("parseAcceptHeader() returned %d preferences, want %d", len(got), tt.want)
			}
		})
	}
}

func TestParseMediaRange(t *testing.T) {
	tests := []struct {
		name       string
		part       string
		wantMIME   string
		wantQValue float64
	}{
		{
			name:       "Simple MIME",
			part:       "image/webp",
			wantMIME:   "image/webp",
			wantQValue: 1.0,
		},
		{
			name:       "With q value",
			part:       "image/webp;q=0.9",
			wantMIME:   "image/webp",
			wantQValue: 0.9,
		},
		{
			name:       "Wildcard",
			part:       "*/*",
			wantMIME:   "*/*",
			wantQValue: 1.0,
		},
		{
			name:       "With q=0",
			part:       "image/webp;q=0",
			wantMIME:   "image/webp",
			wantQValue: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMIME, gotQValue := parseMediaRange(tt.part)
			if gotMIME != tt.wantMIME {
				t.Errorf("parseMediaRange() MIME = %v, want %v", gotMIME, tt.wantMIME)
			}
			if gotQValue != tt.wantQValue {
				t.Errorf("parseMediaRange() qValue = %v, want %v", gotQValue, tt.wantQValue)
			}
		})
	}
}

func TestMimeToFormat(t *testing.T) {
	tests := []struct {
		mime string
		want FormatType
	}{
		{"image/avif", FormatAVIF},
		{"image/webp", FormatWebP},
		{"image/jpeg", ""},
		{"image/png", ""},
		{"application/json", ""},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := mimeToFormat(tt.mime)
			if got != tt.want {
				t.Errorf("mimeToFormat(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}

func TestClientSupports(t *testing.T) {
	tests := []struct {
		name     string
		prefs    []ClientPreference
		format   FormatType
		expected bool
	}{
		{
			name:     "Direct support",
			prefs:    []ClientPreference{{FormatType: FormatWebP, QValue: 1.0}},
			format:   FormatWebP,
			expected: true,
		},
		{
			name:     "QValue is 0",
			prefs:    []ClientPreference{{FormatType: FormatWebP, QValue: 0}},
			format:   FormatWebP,
			expected: false,
		},
		{
			name:     "Wildcard support",
			prefs:    []ClientPreference{{FormatType: "", QValue: 0.8}},
			format:   FormatWebP,
			expected: true,
		},
		{
			name:     "No support",
			prefs:    []ClientPreference{{FormatType: FormatAVIF, QValue: 1.0}},
			format:   FormatWebP,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientSupports(tt.prefs, tt.format)
			if got != tt.expected {
				t.Errorf("clientSupports() = %v, want %v", got, tt.expected)
			}
		})
	}
}
