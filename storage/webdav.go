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
	client     *gowebdav.Client
	httpClient *http.Client
	baseURL    string
	rootPath   string
	username   string
	password   string
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

	// 设置默认超时时间（如果配置中未指定）
	defaultTimeout := cfg.Timeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
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
	client.SetTransport(httpClient.Transport)
	client.SetTimeout(defaultTimeout)

	// 验证连接。gowebdav 不支持给单次请求注入 context，这里依赖底层 http.Client.Timeout。
	if err := testWebDAVConnection(client, rootPath); err != nil {
		return nil, fmt.Errorf("webdav connection test failed: %w", err)
	}

	return &WebDAVStorage{
		client:     client,
		httpClient: httpClient,
		rootPath:   rootPath,
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		username:   cfg.Username,
		password:   cfg.Password,
	}, nil
}

// testWebDAVConnection 测试 WebDAV 连接
func testWebDAVConnection(client *gowebdav.Client, rootPath string) error {
	_, err := client.ReadDir(rootPath)
	return err
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

	if contentLength >= 0 {
		if err := s.client.WriteStreamWithLength(fullPath, file, contentLength, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", storagePath, err)
		}
		return nil
	}

	if err := s.client.WriteStream(fullPath, file, 0644); err != nil {
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

	stream, err := s.client.ReadStream(fullPath)
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

	if err := s.client.Remove(fullPath); err != nil {
		return err
	}
	return nil
}

// Exists 检查文件是否存在
func (s *WebDAVStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	_, err := s.client.Stat(fullPath)
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

	_, err := s.client.ReadDir(s.rootPath)
	return err
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
