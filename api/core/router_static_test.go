package core

import "testing"

// isStaticAPIPath must treat /debug/ as a non-SPA path so that, in production
// (where pprof routes are not registered), unknown /debug/* requests fall to a
// 404 instead of being swallowed by the SPA index.html fallback. This is what
// makes the production-mode smoke test's pprof 404 assertion hold.
func TestIsStaticAPIPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/debug/pprof/heap", true},
		{"/debug/pprof", true},
		{"/api/v1/images", true},
		{"/images/abc", true},
		{"/thumbnails/abc", true},
		{"/system/version", true},
		{"/swagger/index.html", true},
		{"/", false},
		{"/login", false},
		{"/assets/app.js", false},
	}
	for _, tt := range tests {
		if got := isStaticAPIPath(tt.path); got != tt.want {
			t.Errorf("isStaticAPIPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
