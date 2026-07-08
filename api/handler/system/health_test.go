package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
)

// failingStorage 是一个 Health 返回敏感错误的最小存储实现，用于验证健康接口不泄漏底层错误。
type failingStorage struct {
	err error
}

func (f *failingStorage) SaveWithContext(context.Context, string, io.Reader) error { return nil }
func (f *failingStorage) GetWithContext(context.Context, string) (io.ReadSeekCloser, error) {
	return nil, nil
}
func (f *failingStorage) DeleteWithContext(context.Context, string) error { return nil }
func (f *failingStorage) Exists(context.Context, string) (bool, error)    { return false, nil }
func (f *failingStorage) Health(context.Context) error                    { return f.err }
func (f *failingStorage) Name() string                                    { return "failing" }

var _ storage.Provider = (*failingStorage)(nil)

// TestHealthDoesNotLeakStorageError 验证存储故障时响应体只返回通用状态，不暴露底层错误细节。
func TestHealthDoesNotLeakStorageError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "dial tcp 10.1.2.3:9000: secret-internal-detail"
	h := NewHealthHandler(nil, func() storage.Provider {
		return &failingStorage{err: errors.New(secret)}
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/system/health", nil)

	h.Handle(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-internal-detail") {
		t.Fatalf("response body leaked underlying error: %s", w.Body.String())
	}

	var resp struct {
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Checks["storage"] != "error" {
		t.Fatalf("expected storage check = %q, got %q", "error", resp.Checks["storage"])
	}
}

func TestStorageHealthUsesCurrentProvider(t *testing.T) {
	var current storage.Provider
	h := NewHealthHandler(nil, func() storage.Provider { return current })

	if got := h.checkStorageHealth(context.Background()); got != "error: no default storage provider" {
		t.Fatalf("expected unavailable storage, got %q", got)
	}

	current = &failingStorage{}
	if got := h.checkStorageHealth(context.Background()); got != "ok" {
		t.Fatalf("expected recovered storage, got %q", got)
	}
}

// TestLogStatusChangeOnlyOnTransition 验证失败只记录一次、恢复记录一次，重复同状态不刷屏。
func TestLogStatusChangeOnlyOnTransition(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := NewHealthHandler(nil, nil)

	// 连续三次失败应只记录一次。
	h.logStatusChange("storage", "error", errors.New("boom"))
	h.logStatusChange("storage", "error", errors.New("boom"))
	h.logStatusChange("storage", "error", errors.New("boom"))
	if n := strings.Count(buf.String(), "storage health check failed"); n != 1 {
		t.Fatalf("expected failure logged once, got %d:\n%s", n, buf.String())
	}

	// 连续两次恢复应只记录一次。
	h.logStatusChange("storage", "ok", nil)
	h.logStatusChange("storage", "ok", nil)
	if n := strings.Count(buf.String(), "storage health check recovered"); n != 1 {
		t.Fatalf("expected recovery logged once, got %d:\n%s", n, buf.String())
	}
}
