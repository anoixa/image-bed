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
	"github.com/anoixa/image-bed/database/dbcore"
	"github.com/anoixa/image-bed/database/models"
)

// ImageDimensionsTask 图片尺寸提取任务
type ImageDimensionsTask struct {
	Identifier string
	StorageKey string
}

// Execute 执行任务
func (t *ImageDimensionsTask) Execute() {
	// 从本地存储获取图片（异步任务直接使用本地文件系统）
	basePath := config.Get().Server.StorageConfig.Local.Path
	if basePath == "" {
		basePath = "./data/upload"
	}

	filePath := filepath.Join(basePath, t.Identifier)

	// 读取图片数据
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read image file for dimensions extraction: %v", err)
		return
	}

	// 解码图片获取尺寸
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("Failed to decode image for dimensions: %v", err)
		return
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 更新数据库
	db := dbcore.GetDBInstance()
	result := db.Model(&models.Image{}).
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
func ExtractImageDimensionsAsync(identifier, storageKey string) {
	task := &ImageDimensionsTask{
		Identifier: identifier,
		StorageKey: storageKey,
	}
	Submit(task)
}
