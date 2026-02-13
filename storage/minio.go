package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStorage MinIO 存储实现
type MinioStorage struct {
	client     *minio.Client
	bucketName string
}

// NewMinioStorage 创建 MinIO 存储提供者
func NewMinioStorage(cfg config.MinioConfig) (*MinioStorage, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          getOrDefaultInt(cfg.MaxIdleConns, 256),
		MaxIdleConnsPerHost:   getOrDefaultInt(cfg.MaxIdleConnsPerHost, 16),
		IdleConnTimeout:       parseDurationOrDefault(cfg.IdleConnTimeout, time.Minute),
		TLSHandshakeTimeout:   parseDurationOrDefault(cfg.TLSHandshakeTimeout, 10*time.Second),
		ExpectContinueTimeout: 10 * time.Second,
		DisableCompression:    true,
	}

	// SSL 配置
	if cfg.UseSSL {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		if f := os.Getenv("SSL_CERT_FILE"); f != "" {
			rootCAs := mustGetSystemCertPool()
			data, err := os.ReadFile(f)
			if err == nil {
				rootCAs.AppendCertsFromPEM(data)
			}
			transport.TLSClientConfig.RootCAs = rootCAs
		}
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:    cfg.UseSSL,
		Transport: transport,
	})
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
func (s *MinioStorage) SaveWithContext(ctx context.Context, identifier string, file io.Reader) error {
	contentType := "application/octet-stream"

	_, err := s.client.PutObject(ctx, s.bucketName, identifier, file, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload object '%s' to minio: %w", identifier, err)
	}

	return nil
}

// GetWithContext 从 MinIO 获取文件
func (s *MinioStorage) GetWithContext(ctx context.Context, identifier string) (io.ReadSeeker, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, identifier, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return nil, fmt.Errorf("file not found in minio: %s", identifier)
		}
		return nil, fmt.Errorf("failed to get object from minio for '%s': %w", identifier, err)
	}

	return obj, nil
}

// DeleteWithContext 从 MinIO 删除文件
func (s *MinioStorage) DeleteWithContext(ctx context.Context, identifier string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, identifier, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object '%s' from minio: %w", identifier, err)
	}

	return nil
}

// Exists 检查文件是否存在于 MinIO
func (s *MinioStorage) Exists(ctx context.Context, identifier string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucketName, identifier, minio.StatObjectOptions{})
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

// getOrDefaultInt 获取整数值或默认值
func getOrDefaultInt(value int, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

// parseDurationOrDefault 解析持续时间或使用默认值
func parseDurationOrDefault(durationStr string, defaultValue time.Duration) time.Duration {
	if durationStr == "" {
		return defaultValue
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return defaultValue
	}

	return duration
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
