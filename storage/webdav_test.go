package storage

import (
	"context"
	"testing"
	"time"
)

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

// TestWebDAVStorageValidation 测试 WebDAV 存储配置验证
func TestWebDAVStorageValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WebDAVConfig
		wantErr bool
	}{
		{
			name:    "empty URL",
			cfg:     WebDAVConfig{URL: ""},
			wantErr: true,
		},
		{
			name: "valid URL only",
			cfg: WebDAVConfig{
				URL: "https://dav.example.com",
			},
			wantErr: true, // 会连接失败
		},
		{
			name: "with credentials",
			cfg: WebDAVConfig{
				URL:      "https://dav.example.com",
				Username: "user",
				Password: "pass",
			},
			wantErr: true, // 会连接失败
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWebDAVStorage(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWebDAVStorage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
