package async

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"gorm.io/gorm"
)

// MaxFileSizeForDimensions 最大支持提取尺寸的文件大小（10MB）
const MaxFileSizeForDimensions = 10 * 1024 * 1024

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

	// 使用存储提供者读取文件
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

	// 使用 LimitReader 限制读取大小，避免大文件导致 OOM
	limitedReader := io.LimitReader(reader, MaxFileSizeForDimensions)
	
	// 读取数据到内存
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return 0, 0, err
	}

	// 如果读取了 MaxFileSizeForDimensions 字节，可能是大文件，跳过
	if len(data) >= MaxFileSizeForDimensions {
		return 0, 0, nil // 静默跳过，不记录错误
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
	task := &ImageDimensionsTask{
		Identifier: identifier,
		StorageKey: "",
		DB:         db,
		Storage:    storage,
	}
	// 使用带重试的提交，确保任务不被丢弃
	if !TrySubmit(task, 3, 100*time.Millisecond) {
		log.Printf("Failed to submit image dimensions task for %s after retries", identifier)
	}
}
