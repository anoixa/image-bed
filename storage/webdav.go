package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/studio-b12/gowebdav"

	"github.com/anoixa/image-bed/utils/pool"
)

// WebDAVConfig WebDAV 配置结构
type WebDAVConfig struct {
	URL      string
	Username string
	Password string
	RootPath string
	Timeout  time.Duration
}

// WebDAVStorage WebDAV 存储实现
type WebDAVStorage struct {
	client         *gowebdav.Client
	httpClient     *http.Client
	baseURL        string
	rootPath       string
	username       string
	password       string
	defaultTimeout time.Duration // 默认操作超时时间，防止 goroutine 泄漏
}

// NewWebDAVStorage 创建 WebDAV 存储提供者
func NewWebDAVStorage(cfg WebDAVConfig) (*WebDAVStorage, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("webdav URL is required")
	}

	rootPath := cfg.RootPath
	if rootPath != "" {
		rootPath = strings.Trim(rootPath, "/")
		if rootPath != "" {
			rootPath = "/" + rootPath
		}
	}

	// 创建 WebDAV 客户端
	client := gowebdav.NewClient(cfg.URL, cfg.Username, cfg.Password)

	httpClient := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := testWebDAVConnection(ctx, client, rootPath); err != nil {
		return nil, fmt.Errorf("webdav connection test failed: %w", err)
	}

	// 设置默认超时时间（如果配置中未指定）
	defaultTimeout := cfg.Timeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}

	client.SetTimeout(defaultTimeout)

	return &WebDAVStorage{
		client:         client,
		httpClient:     httpClient,
		rootPath:       rootPath,
		baseURL:        strings.TrimRight(cfg.URL, "/"),
		username:       cfg.Username,
		password:       cfg.Password,
		defaultTimeout: defaultTimeout,
	}, nil
}

// testWebDAVConnection 测试 WebDAV 连接
func testWebDAVConnection(ctx context.Context, client *gowebdav.Client, rootPath string) error {
	done := make(chan error, 1)
	go func() {
		// 尝试读取根目录验证连接
		_, err := client.ReadDir(rootPath)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// executeWithTimeout 执行带超时的 WebDAV 操作
func (s *WebDAVStorage) executeWithTimeout(ctx context.Context, operation func() error) error {
	// 使用默认超时创建子上下文
	ctx, cancel := context.WithTimeout(ctx, s.defaultTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- operation()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// executeWithTimeoutResult 执行带超时的 WebDAV 操作并返回结果
func executeWithTimeoutResult[T any](ctx context.Context, timeout time.Duration, operation func() (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		value T
		err   error
	}

	done := make(chan result, 1)
	go func() {
		val, err := operation()
		done <- result{value: val, err: err}
	}()

	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case res := <-done:
		return res.value, res.err
	}
}

// fullPath 生成完整的 WebDAV 路径
func (s *WebDAVStorage) fullPath(storagePath string) string {
	storagePath = strings.TrimLeft(storagePath, "/")
	if s.rootPath != "" {
		return s.rootPath + "/" + storagePath
	}
	return "/" + storagePath
}

// SaveWithContext 保存文件到 WebDAV
func (s *WebDAVStorage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	var contentLength int64 = -1
	if seeker, ok := file.(io.Seeker); ok {
		currentPos, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("failed to get current file position: %w", err)
		}
		endPos, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return fmt.Errorf("failed to determine file size: %w", err)
		}
		if _, err := seeker.Seek(currentPos, io.SeekStart); err != nil {
			return fmt.Errorf("failed to restore file position: %w", err)
		}
		contentLength = endPos - currentPos
	}

	// 执行流式写入（带超时防止 goroutine 泄漏）
	if err := s.executeWithTimeout(ctx, func() error {
		if contentLength >= 0 {
			return s.client.WriteStreamWithLength(fullPath, file, contentLength, 0644)
		}
		return s.client.WriteStream(fullPath, file, 0644)
	}); err != nil {
		return fmt.Errorf("failed to write file %s: %w", storagePath, err)
	}
	return nil
}

// GetWithContext 从 WebDAV 获取文件
func (s *WebDAVStorage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)
	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(config.TempDir, "webdav-get-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}

	stream, err := executeWithTimeoutResult(ctx, s.defaultTimeout, func() (io.ReadCloser, error) {
		return s.client.ReadStream(fullPath)
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to read file %s: %w", storagePath, err)
	}
	defer func() { _ = stream.Close() }()

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	if _, err := io.CopyBuffer(tmp, stream, *bufPtr); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to copy file %s to temp file: %w", storagePath, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to reset temp file for %s: %w", storagePath, err)
	}

	return tmp, nil
}

// DeleteWithContext 从 WebDAV 删除文件
func (s *WebDAVStorage) DeleteWithContext(ctx context.Context, storagePath string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	return s.executeWithTimeout(ctx, func() error {
		return s.client.Remove(fullPath)
	})
}

// Exists 检查文件是否存在
func (s *WebDAVStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	_, err := executeWithTimeoutResult(ctx, s.defaultTimeout, func() (any, error) {
		return s.client.Stat(fullPath)
	})
	if err == nil {
		return true, nil
	}
	// 如果返回 404，文件不存在
	if gowebdav.IsErrNotFound(err) {
		return false, nil
	}
	return false, err
}

// Health 检查存储健康状态
func (s *WebDAVStorage) Health(ctx context.Context) error {
	// 先检查上下文是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 如果 client 为 nil（测试场景），直接返回
	if s.client == nil {
		return nil
	}

	return s.executeWithTimeout(ctx, func() error {
		_, err := s.client.ReadDir(s.rootPath)
		return err
	})
}

// Name 返回存储名称
func (s *WebDAVStorage) Name() string {
	if s.baseURL == "" {
		return "webdav"
	}
	return fmt.Sprintf("webdav:%s%s", s.baseURL, s.rootPath)
}

// buildFileURL 构建文件的完整 URL
func (s *WebDAVStorage) buildFileURL(storagePath string) string {
	fullPath := s.fullPath(storagePath)
	return s.baseURL + fullPath
}

// StreamTo 流式传输到 ResponseWriter
func (s *WebDAVStorage) StreamTo(ctx context.Context, storagePath string, w http.ResponseWriter) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	// 构建完整的文件 URL
	fileURL := s.buildFileURL(storagePath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	if s.username != "" {
		req.SetBasicAuth(s.username, s.password)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 检查响应状态
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("file not found: %s", storagePath)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 设置响应头
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	w.WriteHeader(http.StatusOK)

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	n, err := io.CopyBuffer(w, resp.Body, *bufPtr)
	if err != nil {
		return n, fmt.Errorf("failed to stream file '%s': %w", storagePath, err)
	}

	return n, nil
}
