package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/pool"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// mustGetSystemCertPool 获取系统证书池
func mustGetSystemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		utils.Errorf("Failed to load system cert pool: %v", err)
		return x509.NewCertPool()
	}
	return pool
}

// S3Config S3 兼容配置结构（支持 MinIO、AWS S3、Cloudflare R2 等）
type S3Config struct {
	Type            string `json:"type" binding:"required"`
	Endpoint        string `json:"endpoint" binding:"required"`
	Region          string `json:"region"`
	BucketName      string `json:"bucket_name" binding:"required"`
	AccessKeyID     string `json:"access_key_id" binding:"required"`
	SecretAccessKey string `json:"secret_access_key" binding:"required"`
	ForcePathStyle  bool   `json:"force_path_style"`
	PublicDomain    string `json:"public_domain"`
	IsPrivate       bool   `json:"is_private"`
}

// S3Storage S3 兼容存储实现
type S3Storage struct {
	client         *minio.Client
	bucketName     string
	endpoint       string
	publicDomain   string
	isPrivate      bool
	forcePathStyle bool
}

// NewS3Storage 创建 S3 兼容存储提供者
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	endpoint := cfg.Endpoint
	secure := false

	if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		secure = true
		endpoint = endpoint[8:]
	} else if strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		endpoint = endpoint[7:]
	}

	endpoint = strings.TrimRight(endpoint, "/")

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure,
		Region: cfg.Region,
	}

	if cfg.ForcePathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}

	// SSL 自定义证书配置
	if secure && os.Getenv("SSL_CERT_FILE") != "" {
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
		return nil, fmt.Errorf("failed to initialize S3 client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if bucket '%s' exists: %w", cfg.BucketName, err)
	}
	if !exists {
		err = client.MakeBucket(ctx, cfg.BucketName, minio.MakeBucketOptions{Region: cfg.Region})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket '%s': %w", cfg.BucketName, err)
		}
		utils.Infof("Successfully created bucket: %s", cfg.BucketName)
	}

	return &S3Storage{
		client:         client,
		bucketName:     cfg.BucketName,
		endpoint:       cfg.Endpoint,
		publicDomain:   cfg.PublicDomain,
		isPrivate:      cfg.IsPrivate,
		forcePathStyle: cfg.ForcePathStyle,
	}, nil
}

func (s *S3Storage) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	contentType := getContentTypeFromPathS3(storagePath)
	contentLength, err := getRemainingReaderSize(file)
	if err != nil {
		return fmt.Errorf("failed to determine content length for %q: %w", storagePath, err)
	}

	_, err = s.client.PutObject(ctx, s.bucketName, storagePath, file, contentLength, minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload object '%s' to s3: %w", storagePath, err)
	}

	return nil
}

func getRemainingReaderSize(file io.Reader) (int64, error) {
	seeker, ok := file.(io.Seeker)
	if !ok {
		return -1, nil
	}

	currentPos, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	endPos, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := seeker.Seek(currentPos, io.SeekStart); err != nil {
		return 0, err
	}

	return endPos - currentPos, nil
}

func (s *S3Storage) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return nil, fmt.Errorf("file not found in s3: %s", storagePath)
		}
		return nil, fmt.Errorf("failed to get object from s3 for '%s': %w", storagePath, err)
	}

	return obj, nil
}

func (s *S3Storage) DeleteWithContext(ctx context.Context, storagePath string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, storagePath, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object '%s' from s3: %w", storagePath, err)
	}

	return nil
}

func (s *S3Storage) Exists(ctx context.Context, storagePath string) (bool, error) {
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

func (s *S3Storage) Health(ctx context.Context) error {
	_, err := s.client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("s3 storage health check failed: %w", err)
	}
	return nil
}

func (s *S3Storage) Name() string {
	return "s3"
}

func (s *S3Storage) StreamTo(ctx context.Context, storagePath string, w http.ResponseWriter) (int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in s3: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to get object from s3 for '%s': %w", storagePath, err)
	}
	defer func() { _ = obj.Close() }()

	stat, err := obj.Stat()
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in s3: %s", storagePath)
		}

		if !utils.IsClientDisconnect(err) {
			utils.Errorf("[S3] Stat failed for %s: %v, continuing without Content-Length", storagePath, err)
		}
	} else {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", stat.ContentType)
		}
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))

		if stat.Size > 10*1024*1024 {
			utils.LogIfDevf("[S3] Large file detected: %s (%.2f MB), using optimized streaming", storagePath, float64(stat.Size)/(1024*1024))
		}
	}
	w.WriteHeader(http.StatusOK)

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

func (s *S3Storage) StreamToWithSize(ctx context.Context, storagePath string, w http.ResponseWriter, maxSize int64) (int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, storagePath, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in s3: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to get object from s3 for '%s': %w", storagePath, err)
	}
	defer func() { _ = obj.Close() }()

	stat, err := obj.Stat()
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return 0, fmt.Errorf("file not found in s3: %s", storagePath)
		}
		return 0, fmt.Errorf("failed to stat object: %w", err)
	}

	if maxSize > 0 && stat.Size > maxSize {
		http.Error(w, fmt.Sprintf("file size %d exceeds maximum allowed size %d", stat.Size, maxSize), http.StatusRequestEntityTooLarge)
		return 0, fmt.Errorf("file size %d exceeds maximum allowed size %d", stat.Size, maxSize)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", stat.ContentType)
	}
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size, 10))
	w.WriteHeader(http.StatusOK)

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

func (s *S3Storage) GetDirectURL(storagePath string) string {
	if !s.SupportsDirectLink() {
		return ""
	}

	base := s.publicDomain
	if base == "" {
		base = s.endpoint
	}

	base = strings.TrimRight(base, "/")

	segments := strings.Split(storagePath, "/")
	encodedSegments := make([]string, len(segments))
	for i, seg := range segments {
		encodedSegments[i] = url.PathEscape(seg)
	}
	encodedPath := path.Join(encodedSegments...)

	if s.forcePathStyle || s.publicDomain == "" {
		return fmt.Sprintf("%s/%s/%s", base, s.bucketName, encodedPath)
	}

	return fmt.Sprintf("%s/%s", base, encodedPath)
}

func (s *S3Storage) SupportsDirectLink() bool {
	return !s.isPrivate
}

func (s *S3Storage) ShouldProxy(imageIsPublic bool, globalMode TransferMode) bool {
	if !s.SupportsDirectLink() {
		return true
	}

	switch globalMode {
	case TransferModeAlwaysProxy:
		return true
	case TransferModeAlwaysDirect:
		return false
	case TransferModeAuto, "":
		return !imageIsPublic
	default:
		return true
	}
}

func getContentTypeFromPathS3(storagePath string) string {
	ext := strings.ToLower(filepath.Ext(storagePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".tiff", ".tif":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}
