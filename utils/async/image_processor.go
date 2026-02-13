package async

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// ImageDimensionsTask 图片尺寸提取任务
type ImageDimensionsTask struct {
	Identifier string
	StorageKey string
	DB         *gorm.DB
}

// Execute 执行任务
func (t *ImageDimensionsTask) Execute() {
	if t.DB == nil {
		log.Printf("Database not provided for image dimensions task")
		return
	}

	basePath := config.Get().Server.StorageConfig.Local.Path
	if basePath == "" {
		basePath = "./data/upload"
	}

	filePath := filepath.Join(basePath, t.Identifier)

	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read image file for dimensions extraction: %v", err)
		return
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("Failed to decode image for dimensions: %v", err)
		return
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	result := t.DB.Model(&models.Image{}).
		Where("identifier = ?", t.Identifier).
		UpdateColumns(map[string]interface{}{
			"width":  width,
			"height": height,
		})

	if result.Error != nil {
		log.Printf("Failed to update image dimensions: %v", result.Error)
		return
	}

	if result.RowsAffected > 0 {
		log.Printf("Image dimensions updated: %s = %dx%d", t.Identifier, width, height)
	}
}

// ExtractImageDimensionsAsync 异步提取图片尺寸
func ExtractImageDimensionsAsync(identifier, storageKey string, db *gorm.DB) {
	task := &ImageDimensionsTask{
		Identifier: identifier,
		StorageKey: storageKey,
		DB:         db,
	}
	Submit(task)
}
