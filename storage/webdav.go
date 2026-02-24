package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

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

// fullPath 生成完整的 WebDAV 路径
func (s *WebDAVStorage) fullPath(storagePath string) string {
	storagePath = strings.TrimLeft(storagePath, "/")
	if s.rootPath != "" {
		return s.rootPath + "/" + storagePath
	}
	return "/" + storagePath
}

// ensureParentDir 递归创建父目录
func (s *WebDAVStorage) ensureParentDir(ctx context.Context, fullPath string) error {
	// 获取父目录路径
	parentDir := path.Dir(fullPath)
	
	// 根目录无需创建
	if parentDir == "/" || parentDir == "." {
		return nil
	}
	
	// 逐级分解路径
	parts := strings.Split(strings.Trim(parentDir, "/"), "/")
	currentPath := ""
	
	for _, part := range parts {
		if part == "" {
			continue
		}
		
		if currentPath == "" {
			currentPath = "/" + part
		} else {
			currentPath = currentPath + "/" + part
		}
		
		// 检查目录是否存在，不存在则创建
		done := make(chan error, 1)
		go func(p string) {
			done <- s.client.Mkdir(p, os.FileMode(0755))
		}(currentPath)
		
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil {
				if !isCollectionExistsError(err) {
					return fmt.Errorf("failed to create directory %s: %w", currentPath, err)
				}
			}
		}
	}
	
	return nil
}

// isCollectionExistsError 判断是否为目录已存在的错误
func isCollectionExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 常见 WebDAV 服务器的 "目录已存在" 错误信息
	containsAny := []string{
		"already exists",
		"already exists",
		"conflict",
		"Conflict",
		"409",
		"Method Not Allowed",
		"405",
	}
	for _, s := range containsAny {
		if strings.Contains(errStr, s) {
			return true
		}
	}
	return false
}

// SaveWithContext 保存文件到 WebDAV
func (s *WebDAVStorage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	// 递归创建父目录
	if err := s.ensureParentDir(ctx, fullPath); err != nil {
		return fmt.Errorf("failed to ensure parent directory for %s: %w", storagePath, err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file content: %w", err)
	}

	// 执行写入
	done := make(chan error, 1)
	go func() {
		done <- s.client.Write(fullPath, data, 0644)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", storagePath, err)
		}
		return nil
	}
}

// GetWithContext 从 WebDAV 获取文件
func (s *WebDAVStorage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	type result struct {
		data []byte
		err  error
	}

	done := make(chan result, 1)
	go func() {
		data, err := s.client.Read(fullPath)
		done <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", storagePath, res.err)
		}
		return bytes.NewReader(res.data), nil
	}
}

// DeleteWithContext 从 WebDAV 删除文件
func (s *WebDAVStorage) DeleteWithContext(ctx context.Context, storagePath string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	done := make(chan error, 1)
	go func() {
		done <- s.client.Remove(fullPath)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Exists 检查文件是否存在
func (s *WebDAVStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	fullPath := s.fullPath(storagePath)

	type result struct {
		exists bool
		err    error
	}

	done := make(chan result, 1)
	go func() {
		// 尝试获取文件信息
		_, err := s.client.Stat(fullPath)
		if err == nil {
			done <- result{exists: true, err: nil}
			return
		}
		// 如果返回 404，文件不存在
		if gowebdav.IsErrNotFound(err) {
			done <- result{exists: false, err: nil}
			return
		}
		done <- result{exists: false, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case res := <-done:
		return res.exists, res.err
	}
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
	
	done := make(chan error, 1)
	go func() {
		_, err := s.client.ReadDir(s.rootPath)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
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

// StreamToOptimized 使用 Range 请求支持断点续传的流传输
func (s *WebDAVStorage) StreamToOptimized(ctx context.Context, storagePath string, w http.ResponseWriter, start, end int64) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	// 构建完整的文件 URL
	fileURL := s.buildFileURL(storagePath)

	// 创建 HTTP GET 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加 Range 头支持断点续传
	if end > start {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	} else if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	// 添加认证
	if s.username != "" {
		req.SetBasicAuth(s.username, s.password)
	}

	// 执行请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 检查响应状态
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("file not found: %s", storagePath)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 设置响应头
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}

	// 根据状态码设置响应状态
	if resp.StatusCode == http.StatusPartialContent {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// 使用 buffer pool 复用缓冲区
	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	n, err := io.CopyBuffer(w, resp.Body, *bufPtr)
	if err != nil {
		return n, fmt.Errorf("failed to stream file '%s': %w", storagePath, err)
	}

	return n, nil
}
