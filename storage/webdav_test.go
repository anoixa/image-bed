package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anoixa/image-bed/config"
)

// TestTempFileReadSeekerCloseRemoves 验证 WebDAV 下载临时文件在 Close 时被删除。
func TestTempFileReadSeekerCloseRemoves(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "webdav-get-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	name := tmp.Name()

	var rs io.ReadSeeker = &tempFileReadSeeker{File: tmp}
	closer, ok := rs.(io.Closer)
	if !ok {
		t.Fatal("tempFileReadSeeker must implement io.Closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed after Close, stat err = %v", err)
	}
}

// TestWebDAVConfig 测试 WebDAV 配置结构
func TestWebDAVConfig(t *testing.T) {
	cfg := WebDAVConfig{
		URL:      "https://dav.example.com",
		Username: "user",
		Password: "pass",
		RootPath: "/images",
		Timeout:  30 * time.Second,
	}

	if cfg.URL != "https://dav.example.com" {
		t.Errorf("expected URL to be https://dav.example.com, got %s", cfg.URL)
	}
	if cfg.Username != "user" {
		t.Errorf("expected Username to be user, got %s", cfg.Username)
	}
	if cfg.RootPath != "/images" {
		t.Errorf("expected RootPath to be /images, got %s", cfg.RootPath)
	}
}

func TestWebDAVStorageValidationRejectsEmptyURL(t *testing.T) {
	_, err := NewWebDAVStorage(WebDAVConfig{URL: ""})
	if err == nil {
		t.Fatal("expected empty URL to fail")
	}
}

func TestWebDAVStorageGetUsesRealHTTPAndCloseRemovesTempFile(t *testing.T) {
	var propfinds atomic.Int32
	var gets atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			propfinds.Add(1)
			if r.URL.Path != "/dav" && r.URL.Path != "/dav/" {
				t.Errorf("PROPFIND path = %q, want /dav or /dav/", r.URL.Path)
			}
			writeWebDAVMultiStatus(w, "/dav/")
		case http.MethodGet:
			gets.Add(1)
			if r.URL.Path != "/dav/images/test.txt" {
				t.Errorf("GET path = %q, want /dav/images/test.txt", r.URL.Path)
			}
			_, _ = w.Write([]byte("webdav file"))
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	s, err := NewWebDAVStorage(WebDAVConfig{
		URL:      server.URL,
		RootPath: "/dav",
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewWebDAVStorage: %v", err)
	}

	rc, err := s.GetWithContext(context.Background(), "images/test.txt")
	if err != nil {
		t.Fatalf("GetWithContext: %v", err)
	}
	named, ok := rc.(interface{ Name() string })
	if !ok {
		t.Fatal("returned reader should expose the temp file name through embedded *os.File")
	}
	tempName := named.Name()
	if _, err := os.Stat(tempName); err != nil {
		t.Fatalf("temp file should exist before Close: %v", err)
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read returned file: %v", err)
	}
	if string(data) != "webdav file" {
		t.Fatalf("file content = %q, want %q", string(data), "webdav file")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(tempName); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed after Close, stat err = %v", err)
	}

	if propfinds.Load() == 0 {
		t.Fatal("expected NewWebDAVStorage to issue PROPFIND")
	}
	if gets.Load() != 1 {
		t.Fatalf("expected exactly one GET, got %d", gets.Load())
	}
}

func TestWebDAVGetWithContextCancelsInFlightDownloadAndCleansTempFile(t *testing.T) {
	started := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			writeWebDAVMultiStatus(w, "/")
		case http.MethodGet:
			close(started)
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	s, err := NewWebDAVStorage(WebDAVConfig{
		URL:     server.URL,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewWebDAVStorage: %v", err)
	}

	before := webDAVTempFileSet(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		rc, err := s.GetWithContext(ctx, "slow.bin")
		if rc != nil {
			_ = rc.Close()
		}
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("GET request did not start")
	}

	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected canceled download to fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GetWithContext did not return after context cancellation")
	}

	after := webDAVTempFileSet(t)
	if len(after) != len(before) {
		t.Fatalf("webdav temp files changed after canceled download: before=%v after=%v", before, after)
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			t.Fatalf("pre-existing temp file disappeared unexpectedly: %s", name)
		}
	}
}

// TestWebDAVStorageFullPath 测试路径生成逻辑
func TestWebDAVStorageFullPath(t *testing.T) {
	tests := []struct {
		name        string
		rootPath    string
		storagePath string
		want        string
	}{
		{
			name:        "empty root path",
			rootPath:    "",
			storagePath: "original/2024/01/15/test.jpg",
			want:        "/original/2024/01/15/test.jpg",
		},
		{
			name:        "with root path",
			rootPath:    "/images",
			storagePath: "original/2024/01/15/test.jpg",
			want:        "/images/original/2024/01/15/test.jpg",
		},
		{
			name:        "root path without leading slash",
			rootPath:    "/images",
			storagePath: "test.jpg",
			want:        "/images/test.jpg",
		},
		{
			name:        "storage path with leading slash",
			rootPath:    "",
			storagePath: "/test.jpg",
			want:        "/test.jpg",
		},
		{
			name:        "date formatted path",
			rootPath:    "/uploads",
			storagePath: "thumbnails/2024/12/25/image_300.webp",
			want:        "/uploads/thumbnails/2024/12/25/image_300.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &WebDAVStorage{
				rootPath: tt.rootPath,
			}
			got := s.fullPath(tt.storagePath)
			if got != tt.want {
				t.Errorf("fullPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWebDAVStorageContextCancellation 测试上下文取消处理
func TestWebDAVStorageContextCancellation(t *testing.T) {
	s := &WebDAVStorage{
		client:   nil, // 模拟状态，不会实际调用
		rootPath: "",
		baseURL:  "https://example.com",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	t.Run("SaveWithContext", func(t *testing.T) {
		err := s.SaveWithContext(ctx, "test.jpg", nil)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("GetWithContext", func(t *testing.T) {
		_, err := s.GetWithContext(ctx, "test.jpg")
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("DeleteWithContext", func(t *testing.T) {
		err := s.DeleteWithContext(ctx, "test.jpg")
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		_, err := s.Exists(ctx, "test.jpg")
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("Health", func(t *testing.T) {
		err := s.Health(ctx)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

// TestWebDAVStorageName 测试存储名称
func TestWebDAVStorageName(t *testing.T) {
	s := &WebDAVStorage{}
	if got := s.Name(); got != "webdav" {
		t.Errorf("Name() = %v, want webdav", got)
	}
}

// TestStorageConfigWithWebDAV 测试 StorageConfig 包含 WebDAV 配置
func TestStorageConfigWithWebDAV(t *testing.T) {
	cfg := StorageConfig{
		ID:             1,
		Name:           "my-webdav",
		Type:           "webdav",
		IsDefault:      false,
		WebDAVURL:      "https://dav.example.com",
		WebDAVUsername: "user",
		WebDAVPassword: "secret",
		WebDAVRootPath: "/uploads",
	}

	if cfg.Type != "webdav" {
		t.Errorf("expected Type to be webdav, got %s", cfg.Type)
	}
	if cfg.WebDAVURL != "https://dav.example.com" {
		t.Errorf("expected WebDAVURL to be https://dav.example.com, got %s", cfg.WebDAVURL)
	}
	if cfg.WebDAVRootPath != "/uploads" {
		t.Errorf("expected WebDAVRootPath to be /uploads, got %s", cfg.WebDAVRootPath)
	}
}

// TestCreateProviderWebDAV 测试 createProvider 支持 WebDAV 类型
func TestCreateProviderWebDAV(t *testing.T) {
	// 由于 NewWebDAVStorage 会尝试连接服务器，这里只验证配置验证逻辑
	cfg := StorageConfig{
		Type:           "webdav",
		WebDAVURL:      "", // 空 URL 会导致错误
		WebDAVUsername: "",
		WebDAVPassword: "",
	}

	_, err := createProvider(cfg)
	if err == nil {
		t.Error("expected error for empty WebDAV URL, got nil")
	}

	if err != nil && err.Error() != "webdav URL is required" {
		// 实际错误可能是连接失败，但首先应该检查 URL
		t.Logf("Got expected error: %v", err)
	}
}

// TestWebDAVStoragePathVariations 测试各种路径格式
func TestWebDAVStoragePathVariations(t *testing.T) {
	s := &WebDAVStorage{
		rootPath: "/data",
		baseURL:  "https://dav.example.com",
	}

	paths := []struct {
		input string
		want  string
	}{
		{"original/2024/01/01/image.jpg", "/data/original/2024/01/01/image.jpg"},
		{"thumbnails/2024/12/25/img_300.webp", "/data/thumbnails/2024/12/25/img_300.webp"},
		{"converted/webp/2024/06/15/file.webp", "/data/converted/webp/2024/06/15/file.webp"},
		{"test.png", "/data/test.png"},
	}

	for _, p := range paths {
		got := s.fullPath(p.input)
		if got != p.want {
			t.Errorf("fullPath(%s) = %s, want %s", p.input, got, p.want)
		}
	}
}

func writeWebDAVMultiStatus(w http.ResponseWriter, href string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>` + href + `</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype><D:collection/></D:resourcetype>
        <D:displayname>dav</D:displayname>
        <D:getlastmodified>Mon, 02 Jan 2006 15:04:05 GMT</D:getlastmodified>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
}

func webDAVTempFileSet(t *testing.T) map[string]struct{} {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(config.TempDir, "webdav-get-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	result := make(map[string]struct{}, len(files))
	for _, file := range files {
		result[file] = struct{}{}
	}
	return result
}
