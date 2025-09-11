package storage

import (
	"io"
	"log"

	"github.com/anoixa/image-bed/config"
)

var AppStorage Storage

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
	storageType := cfg.Server.StorageConfig.Type
	var err error

	log.Printf("Initializing storage, type: %s", storageType)

	switch storageType {
	case "local":
		AppStorage, err = newLocalStorage(cfg.Server.StorageConfig.Local.Path)
		if err != nil {
			log.Println("failed initialized Local storage.")
		} else {
			log.Println("successfully initialized Local storage.")
		}
	case "minio":
		initMinioClient()
		AppStorage = newMinioStorage()
		log.Println("Successfully initialized MinIO storage.")
	default:
		log.Fatalf("Invalid storage type specified in config: %s", storageType)
	}
}
