package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	minioClient *minio.Client
	once        sync.Once
)

type minioStorage struct {
	bucketName         string
	presignedURLExpiry time.Duration
}

// initMinioClient 使用 sync.Once 确保 MinIO 客户端在程序生命周期中只被初始化一次。
func initMinioClient() {
	once.Do(func() {
		cfg := config.Get()
		client, err := minio.New(cfg.Server.StorageConfig.Minio.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.Server.StorageConfig.Minio.AccessKeyID, cfg.Server.StorageConfig.Minio.SecretAccessKey, ""),
			Secure: cfg.Server.StorageConfig.Minio.UseSSL,
		})
		if err != nil {
			log.Fatalf("Failed to initialize MinIO client: %v", err)
		}

		ctx := context.Background()
		bucketName := cfg.Server.StorageConfig.Minio.BucketName
		exists, err := client.BucketExists(ctx, bucketName)
		if err != nil {
			log.Fatalf("Failed to check if bucket exists: %v", err)
		}
		if !exists {
			err = client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
			if err != nil {
				log.Fatalf("Failed to create bucket '%s': %v", bucketName, err)
			}
			log.Printf("Successfully created bucket: %s", bucketName)
		}
		minioClient = client
		log.Println("MinIO client singleton initialized successfully.")
	})
}

// getMinioClient get minio client
func getMinioClient() *minio.Client {
	if minioClient == nil {
		log.Fatal("MinIO client is not initialized. Call storage.StorageInit() first.")
	}
	return minioClient
}

// newMinioStorage
func newMinioStorage() *minioStorage {
	cfg := config.Get()
	if cfg.Server.StorageConfig.Minio.BucketName == "" {
		panic("MinIO bucket name is not configured")
	}
	return &minioStorage{
		bucketName:         cfg.Server.StorageConfig.Minio.BucketName,
		presignedURLExpiry: 24 * time.Hour,
	}
}

// Save 方法将文件上传到 MinIO。
func (s *minioStorage) Save(identifier string, file io.Reader) error {
	client := getMinioClient()
	objectName := identifier

	contentType := "application/octet-stream"

	_, err := client.PutObject(context.Background(), s.bucketName, objectName, file, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload object '%s' to minio: %w", objectName, err)
	}

	return nil
}

// Get Get image
func (s *minioStorage) Get(identifier string) (io.ReadCloser, error) {
	client := getMinioClient()

	obj, err := client.GetObject(context.Background(), s.bucketName, identifier, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return nil, fmt.Errorf("file not found in minio: %s", identifier)
		}
		return nil, fmt.Errorf("failed to get object stream from minio for '%s': %w", identifier, err)
	}

	return obj, nil
}
