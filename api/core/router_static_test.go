package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

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

func TestStaticRoutesSetCacheHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	staticFS := http.FS(fstest.MapFS{
		"index.html":               {Data: []byte("<html>app</html>")},
		"assets/index-deadbeef.js": {Data: []byte("console.log('app')")},
	})
	registerStaticRoutesWithFS(router, staticFS)

	indexRecorder := httptest.NewRecorder()
	indexRequest, _ := http.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(indexRecorder, indexRequest)

	if got := indexRecorder.Code; got != http.StatusOK {
		t.Fatalf("GET /login status = %d, want %d", got, http.StatusOK)
	}
	if got := indexRecorder.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("GET /login Cache-Control = %q", got)
	}

	assetRecorder := httptest.NewRecorder()
	assetRequest, _ := http.NewRequest(http.MethodGet, "/assets/index-deadbeef.js", nil)
	router.ServeHTTP(assetRecorder, assetRequest)

	if got := assetRecorder.Code; got != http.StatusOK {
		t.Fatalf("GET asset status = %d, want %d", got, http.StatusOK)
	}
	if got := assetRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("GET asset Cache-Control = %q", got)
	}
	if got := assetRecorder.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("GET asset Content-Type = %q", got)
	}
}

func TestStaticRoutesHandleHeadAndMissingFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	staticFS := http.FS(fstest.MapFS{
		"index.html":               {Data: []byte("<html>app</html>")},
		"assets/index-deadbeef.js": {Data: []byte("console.log('app')")},
	})
	registerStaticRoutesWithFS(router, staticFS)

	headRecorder := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/assets/index-deadbeef.js", nil)
	router.ServeHTTP(headRecorder, headRequest)
	if got := headRecorder.Code; got != http.StatusOK {
		t.Fatalf("HEAD asset status = %d, want %d", got, http.StatusOK)
	}
	if got := headRecorder.Body.Len(); got != 0 {
		t.Fatalf("HEAD asset body length = %d, want 0", got)
	}

	missingRecorder := httptest.NewRecorder()
	missingRequest := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	router.ServeHTTP(missingRecorder, missingRequest)
	if got := missingRecorder.Code; got != http.StatusNotFound {
		t.Fatalf("GET missing asset status = %d, want %d", got, http.StatusNotFound)
	}

	apiRecorder := httptest.NewRecorder()
	apiRequest := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	router.ServeHTTP(apiRecorder, apiRequest)
	if got := apiRecorder.Code; got != http.StatusNotFound {
		t.Fatalf("GET missing API status = %d, want %d", got, http.StatusNotFound)
	}
}
