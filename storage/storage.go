package storage

import (
	"log"
	"mime/multipart"

	"github.com/anoixa/image-bed/config"
)

var AppStorage Storage

type Storage interface {
	Save(file multipart.File, header *multipart.FileHeader) (string, error)
	Get(identifier string) (string, error)
}

func InitStorage() {
	cfg := config.Get()
	storageType := cfg.Server.StorageConfig.Type

	log.Printf("Initializing storage, type: %s", storageType)

	switch storageType {
	case "local":
		AppStorage = newLocalStorage(cfg.Server.StorageConfig.Local.Path)
		log.Println("Successfully initialized Local storage.")
	case "minio":
		initMinioClient()
		AppStorage = newMinioStorage()
		log.Println("Successfully initialized MinIO storage.")
	default:
		log.Fatalf("Invalid storage type specified in config: %s", storageType)
	}
}
