package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/pool"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioConfig MinIO 配置结构
type MinioConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
	// 直链配置
	EnableDirectLink bool
	PublicEndpoint   string
	IsPublicBucket   bool
	ForceProxy       bool
	TransferMode     TransferMode
}

// MinioStorage MinIO 存储实现
type MinioStorage struct {
	client           *minio.Client
	bucketName       string
	enableDirectLink bool
	publicEndpoint   string
	isPublicBucket   bool
	forceProxy       bool
	transferMode     TransferMode
}

// NewMinioStorage 创建 MinIO 存储提供者
func NewMinioStorage(cfg MinioConfig) (*MinioStorage, error) {
	// 清理 endpoint，去除 scheme 前缀（http:// 或 https://）
	endpoint := cfg.Endpoint
	if strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		endpoint = endpoint[7:]
	} else if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		endpoint = endpoint[8:]
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	}

	// SSL 自定义证书配置
	if cfg.UseSSL && os.Getenv("SSL_CERT_FILE") != "" {
		rootCAs := mustGetSystemCertPool()
		if data, err := os.ReadFile(os.Getenv("SSL_CERT_FILE")); err == nil {
			rootCAs.AppendCertsFromPEM(data)
		}
		opts.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    rootCAs,
			},
		}
	}

	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if bucket '%s' exists: %w", cfg.BucketName, err)
	}
	if !exists {
		err = client.MakeBucket(ctx, cfg.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket '%s': %w", cfg.BucketName, err)
		}
		log.Printf("Successfully created bucket: %s", cfg.BucketName)
	}

	return &MinioStorage{
		client:           client,
		bucketName:       cfg.BucketName,
		enableDirectLink: cfg.EnableDirectLink,
		publicEndpoint:   cfg.PublicEndpoint,
		isPublicBucket:   cfg.IsPublicBucket,
		forceProxy:       cfg.ForceProxy,
		transferMode:     cfg.TransferMode,
	}, nil
}

// SaveWithContext 保存文件到 MinIO
func (s *MinioStorage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	contentType := "application/octet-stream"

	_, err := s.client.PutObject(ctx, s.bucketName, storagePath, file, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload object '%s' to minio: %w", storagePath, err)
	}

	return nil
}

// GetWithContext 从 MinIO 获取文件
func (s *MinioStorage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return nil, fmt.Errorf("file not found in minio: %s", storagePath)
		}
		return nil, fmt.Errorf("failed to get object from minio for '%s': %w", storagePath, err)
	}

	return obj, nil
}

// DeleteWithContext 从 MinIO 删除文件
func (s *MinioStorage) DeleteWithContext(ctx context.Context, storagePath string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, storagePath, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object '%s' from minio: %w", storagePath, err)
	}

	return nil
}

// Exists 检查文件是否存在于 MinIO
func (s *MinioStorage) Exists(ctx context.Context, storagePath string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucketName, storagePath, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Health 检查 MinIO 健康状态
func (s *MinioStorage) Health(ctx context.Context) error {
	_, err := s.client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("minio storage health check failed: %w", err)
	}
	return nil
}

// Name 返回存储名称
func (s *MinioStorage) Name() string {
	return "minio"
}

// StreamTo 将 MinIO 对象直接流式传输到 ResponseWriter
// 避免全量加载到内存，使用 buffer pool 复用缓冲区
// 对于大文件（>10MB），使用更大的缓冲区并进行分块传输
func (s *MinioStorage) StreamTo(ctx context.Context, storagePath string, w http.ResponseWriter) (int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in minio: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to get object from minio for '%s': %w", storagePath, err)
	}
	defer func() { _ = obj.Close() }()

	stat, err := obj.Stat()
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in minio: %s", storagePath)
		}

		if !utils.IsClientDisconnect(err) {
			log.Printf("[MinIO] Stat failed for %s: %v, continuing without Content-Length", storagePath, err)
		}
	} else {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", stat.ContentType)
		}
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))

		// 对于大文件（>10MB），记录日志并使用更大的缓冲区
		if stat.Size > 10*1024*1024 {
			utils.LogIfDevf("[MinIO] Large file detected: %s (%.2f MB), using optimized streaming", storagePath, float64(stat.Size)/(1024*1024))
		}
	}
	w.WriteHeader(http.StatusOK)

	// 使用 buffer pool 复用缓冲区（默认 32KB）
	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	n, err := io.CopyBuffer(w, obj, *bufPtr)
	if err != nil {
		if utils.IsClientDisconnect(err) {
			return n, err
		}
		return n, fmt.Errorf("failed to stream object '%s': %w", storagePath, err)
	}

	return n, nil
}

// StreamToWithSize 将 MinIO 对象流式传输到指定 writer，支持文件大小检查
// 对于超过 maxSize 的文件，返回错误而不传输
func (s *MinioStorage) StreamToWithSize(ctx context.Context, storagePath string, w http.ResponseWriter, maxSize int64) (int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in minio: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to get object from minio for '%s': %w", storagePath, err)
	}
	defer func() { _ = obj.Close() }()

	stat, err := obj.Stat()
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in minio: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to stat object: %w", err)
	}

	// 检查文件大小限制
	if maxSize > 0 && stat.Size > maxSize {
		http.Error(w, fmt.Sprintf("file size %d exceeds maximum allowed size %d", stat.Size, maxSize), http.StatusRequestEntityTooLarge)
		return 0, fmt.Errorf("file size %d exceeds maximum allowed size %d", stat.Size, maxSize)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", stat.ContentType)
	}
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))
	w.WriteHeader(http.StatusOK)

	// 使用 buffer pool 复用缓冲区
	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	n, err := io.CopyBuffer(w, obj, *bufPtr)
	if err != nil {
		if utils.IsClientDisconnect(err) {
			return n, err
		}
		return n, fmt.Errorf("failed to stream object '%s': %w", storagePath, err)
	}

	return n, nil
}

// mustGetSystemCertPool 获取系统证书池
func mustGetSystemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Printf("Failed to load system cert pool: %v", err)
		return x509.NewCertPool()
	}
	return pool
}

// === DirectURLProvider 接口实现 ===

// GetDirectURL 获取直链 URL
func (s *MinioStorage) GetDirectURL(storagePath string) string {
	if !s.SupportsDirectLink() {
		return ""
	}

	// 构建 URL
	base := s.publicEndpoint
	if base == "" {
		// 使用 MinIO 端点
		base = s.client.EndpointURL().String()
	}

	// 确保格式正确
	base = strings.TrimRight(base, "/")

	// URL 编码路径（处理中文、空格等）
	segments := strings.Split(storagePath, "/")
	encodedSegments := make([]string, len(segments))
	for i, seg := range segments {
		encodedSegments[i] = url.PathEscape(seg)
	}
	encodedPath := path.Join(encodedSegments...)

	return fmt.Sprintf("%s/%s/%s", base, s.bucketName, encodedPath)
}

// SupportsDirectLink 是否支持直链
func (s *MinioStorage) SupportsDirectLink() bool {
	// 必须启用直链且是 public bucket 且不强制代理
	return s.enableDirectLink && s.isPublicBucket && !s.forceProxy
}

// ShouldProxy 根据策略判断是否走代理
func (s *MinioStorage) ShouldProxy(imageIsPublic bool, globalMode TransferMode) bool {
	// 强制代理
	if s.forceProxy {
		return true
	}

	// 使用存储级策略，如果未设置则使用全局策略
	mode := s.transferMode
	if mode == "" {
		mode = globalMode
	}

	switch mode {
	case TransferModeAlwaysProxy:
		// 总是代理
		return true

	case TransferModeAlwaysDirect:
		// 总是直链（仅限 public bucket 配置正确时）
		return !s.SupportsDirectLink()

	case TransferModeAuto, "": // 默认 auto
		// 自动：私有图片代理，公开图片直链
		if !imageIsPublic {
			return true
		}
		return !s.SupportsDirectLink()

	default:
		// 未知模式，安全起见走代理
		return true
	}
}
