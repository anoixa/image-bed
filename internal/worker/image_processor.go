package worker

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"gorm.io/gorm"
)

// ImageDimensionsTask 图片尺寸提取任务
type ImageDimensionsTask struct {
	Identifier string
	StorageKey string
	DB         *gorm.DB
	Storage    storage.Provider // 存储提供者，支持任意存储后端
}

// Execute 执行任务
func (t *ImageDimensionsTask) Execute() {
	if t.DB == nil {
		log.Printf("Database not provided for image dimensions task")
		return
	}

	var existing models.Image
	if err := t.DB.Select("width", "height").Where("identifier = ?", t.Identifier).First(&existing).Error; err == nil {
		if existing.Width > 0 && existing.Height > 0 {
			return
		}
	}

	width, height, err := t.extractFromStorage()

	if err != nil {
		log.Printf("Failed to extract image dimensions for %s: %v", t.Identifier, err)
		return
	}

	// 更新数据库
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

// extractFromStorage 从存储提供者读取并提取尺寸
func (t *ImageDimensionsTask) extractFromStorage() (int, int, error) {
	ctx := context.Background()

	// 获取文件流
	reader, err := t.Storage.GetWithContext(ctx, t.Identifier)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closer, ok := reader.(io.Closer); ok {
			closer.Close()
		}
	}()

	// 读取数据到内存
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, 0, err
	}

	return decodeImageDimensions(data)
}

// decodeImageDimensions 解码图片并提取尺寸
func decodeImageDimensions(data []byte) (int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}

	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), nil
}

// ExtractImageDimensionsAsync 异步提取图片尺寸
func ExtractImageDimensionsAsync(identifier string, storageConfigID uint, db *gorm.DB, storage storage.Provider) {
	pool := GetGlobalPool()
	if pool == nil {
		return
	}

	ok := pool.Submit(func() {
		task := &ImageDimensionsTask{
			Identifier: identifier,
			StorageKey: "",
			DB:         db,
			Storage:    storage,
		}
		task.Execute()
	})

	if !ok {
		log.Printf("Failed to submit image dimensions task for %s", identifier)
	}
}
