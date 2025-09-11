package storage

import (
	"fmt"
	"io"
	"log"

	"github.com/anoixa/image-bed/config"
)

var storageClients = make(map[string]Storage)
var defaultStorageType string

type ImageStream struct {
	Reader      io.ReadCloser
	ContentType string
	Size        int64
}

type Storage interface {
	Save(identifier string, file io.Reader) error
	Get(identifier string) (io.ReadCloser, error)
	Delete(identifier string) error
}

func InitStorage(cfg *config.Config) {
	log.Println("Initializing available storage backends...")

	if cfg.Server.StorageConfig.Local.Path != "" {
		localClient, err := newLocalStorage(cfg.Server.StorageConfig.Local.Path)
		if err != nil {
			log.Printf("Failed to initialize Local storage: %v", err)
		} else {
			storageClients["local"] = localClient
			log.Println("Successfully initialized and registered 'local' storage.")
		}
	}

	if cfg.Server.StorageConfig.Minio.Endpoint != "" {
		minioClient, err := newMinioClient(cfg.Server.StorageConfig.Minio)
		if err != nil {
			log.Printf("Failed to initialize MinIO storage: %v", err)
		} else {
			storageClients["minio"] = minioClient
			log.Println("Successfully initialized and registered 'minio' storage.")
		}
	}

	// 设置默认存储
	defaultStorageType = cfg.Server.StorageConfig.Type
	if _, ok := storageClients[defaultStorageType]; !ok {
		log.Fatalf("Default storage type '%s' is specified in config but failed to initialize.", defaultStorageType)
	}
	log.Printf("Default storage is set to: '%s'", defaultStorageType)

	if len(storageClients) == 0 {
		log.Fatalln("No storage backends were successfully initialized. Application cannot start.")
	}
}

// GetStorage 获取储存后端
func GetStorage(name string) (Storage, error) {
	if name == "" {
		name = defaultStorageType
	}

	client, ok := storageClients[name]
	if !ok {
		return nil, fmt.Errorf("storage backend '%s' not found or not configured", name)
	}
	return client, nil
}
