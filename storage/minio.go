package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/anoixa/image-bed/utils/pool"
)

// MinioConfig MinIO 配置结构
type MinioConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
}

// MinioStorage MinIO 存储实现
type MinioStorage struct {
	client     *minio.Client
	bucketName string
}

// NewMinioStorage 创建 MinIO 存储提供者
func NewMinioStorage(cfg MinioConfig) (*MinioStorage, error) {
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

	client, err := minio.New(cfg.Endpoint, opts)
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
		client:     client,
		bucketName: cfg.BucketName,
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

	// 获取对象元数据
	stat, err := obj.Stat()
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in minio: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to stat object from minio for '%s': %w", storagePath, err)
	}

	// 设置响应头
	w.Header().Set("Content-Type", stat.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))
	w.WriteHeader(http.StatusOK)

	// 使用 buffer pool 复用缓冲区（默认 32KB）
	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	n, err := io.CopyBuffer(w, obj, *bufPtr)
	if err != nil {
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
