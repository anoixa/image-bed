package storage

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"path/filepath"
	"sync"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/google/uuid"
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

// getMinioClient 安全地获取已初始化的 MinIO 客户端。
func getMinioClient() *minio.Client {
	if minioClient == nil {
		log.Fatal("MinIO client is not initialized. Call storage.StorageInit() first.")
	}
	return minioClient
}

// newMinioStorage 是 minioStorage 的构造函数。
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
func (s *minioStorage) Save(file multipart.File, header *multipart.FileHeader) (string, error) {
	client := getMinioClient()
	ext := filepath.Ext(header.Filename)
	// TODO 替换为时间戳
	objectName := uuid.New().String() + ext
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := client.PutObject(context.Background(), s.bucketName, objectName, file, header.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload object to minio: %w", err)
	}
	return objectName, nil
}

// Get 方法为 MinIO 中的对象生成一个预签名的 URL。
func (s *minioStorage) Get(filename string) (string, error) {
	client := getMinioClient()
	_, err := client.StatObject(context.Background(), s.bucketName, filename, minio.StatObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("file not found in minio bucket '%s': %s", s.bucketName, filename)
	}

	presignedURL, err := client.PresignedGetObject(context.Background(), s.bucketName, filename, s.presignedURLExpiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned url for %s: %w", filename, err)
	}
	return presignedURL.String(), nil
}
