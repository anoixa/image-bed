package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioStorage struct {
	client             *minio.Client
	bucketName         string
	presignedURLExpiry time.Duration
}

// newMinioStorage
func newMinioClient(cfg config.MinioConfig) (*minioStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
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

	storage := &minioStorage{
		client:             client, // 在 struct 中持有 client
		bucketName:         cfg.BucketName,
		presignedURLExpiry: 24 * time.Hour,
	}

	return storage, nil
}

// Save 将文件上传到 MinIO。
func (s *minioStorage) Save(identifier string, file io.Reader) error {
	objectName := identifier

	contentType := "application/octet-stream"

	_, err := s.client.PutObject(context.Background(), s.bucketName, objectName, file, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload object '%s' to minio: %w", objectName, err)
	}

	return nil
}

// Get Get image
func (s *minioStorage) Get(identifier string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(context.Background(), s.bucketName, identifier, minio.GetObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return nil, fmt.Errorf("file not found in minio: %s", identifier)
		}
		return nil, fmt.Errorf("failed to get object stream from minio for '%s': %w", identifier, err)
	}

	return obj, nil
}

// Delete
func (s *minioStorage) Delete(identifier string) error {
	objectName := identifier

	err := s.client.RemoveObject(context.Background(), s.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object '%s' from minio: %w", objectName, err)
	}

	return nil
}
