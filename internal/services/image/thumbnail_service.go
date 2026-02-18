package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/services/config"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils/async"
	"gorm.io/gorm"
)

// ThumbnailResult 缩略图结果
type ThumbnailResult struct {
	Format     string
	Identifier string
	Width      int
	Height     int
	MIMEType   string
}

// ThumbnailService 缩略图服务
type ThumbnailService struct {
	variantRepo   images.VariantRepository
	configManager *config.Manager
	storage       storage.Provider
	converter     *Converter
}

// NewThumbnailService 创建缩略图服务
func NewThumbnailService(
	repo images.VariantRepository,
	cm *config.Manager,
	storage storage.Provider,
	converter *Converter,
) *ThumbnailService {
	return &ThumbnailService{
		variantRepo:   repo,
		configManager: cm,
		storage:       storage,
		converter:     converter,
	}
}

// GetThumbnail 获取缩略图信息
// 如果缩略图不存在，返回 nil，调用方需要触发生成
func (s *ThumbnailService) GetThumbnail(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, error) {
	format := models.GetThumbnailFormat(width)

	// 查询是否存在该尺寸的缩略图
	variant, err := s.variantRepo.GetVariantByImageIDAndFormat(image.ID, format)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 缩略图不存在
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get thumbnail variant: %w", err)
	}

	// 缩略图生成未成功
	if variant.Status != models.VariantStatusCompleted {
		return nil, nil
	}

	return &ThumbnailResult{
		Format:     format,
		Identifier: variant.Identifier,
		Width:      variant.Width,
		Height:     variant.Height,
		MIMEType:   s.getMIMETypeFromFormat(format),
	}, nil
}

// TriggerGeneration 触发缩略图生成
func (s *ThumbnailService) TriggerGeneration(image *models.Image, width int) {
	ctx := context.Background()

	// 读取配置，失败时使用默认配置
	settings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		log.Printf("[Thumbnail] Failed to get settings, using defaults: %v", err)
		settings = config.DefaultThumbnailSettings()
	}

	// 检查缩略图是否启用
	if !settings.Enabled {
		log.Printf("[Thumbnail] Thumbnail generation disabled")
		return
	}

	// 检查是否为有效尺寸
	if !models.IsValidThumbnailWidth(width, settings.Sizes) {
		log.Printf("[Thumbnail] Invalid thumbnail width: %d", width)
		return
	}

	format := models.GetThumbnailFormat(width)

	// 创建或获取变体记录
	variant, err := s.variantRepo.UpsertPending(image.ID, format)
	if err != nil {
		log.Printf("[Thumbnail] Failed to upsert variant: %v", err)
		return
	}

	// 只有 pending 状态才提交任务
	if variant.Status != models.VariantStatusPending {
		log.Printf("[Thumbnail] Variant %d status=%s, skip submission", variant.ID, variant.Status)
		return
	}

	// 检查重试次数
	if variant.RetryCount >= settings.MaxRetries {
		log.Printf("[Thumbnail] Variant %d reached max retries", variant.ID)
		return
	}

	// 生成缩略图标识
	thumbnailIdentifier := s.GenerateThumbnailIdentifier(image.Identifier, width)

	// 提交任务
	task := &async.ThumbnailTask{
		VariantID:        variant.ID,
		ImageID:          image.ID,
		SourceIdentifier: image.Identifier,
		TargetIdentifier: thumbnailIdentifier,
		TargetWidth:      width,
		ConfigManager:    s.configManager,
		VariantRepo:      s.variantRepo,
		Storage:          s.storage,
	}

	if !async.TrySubmit(task, 3, 100*time.Millisecond) {
		log.Printf("[Thumbnail] Failed to submit task for %s", image.Identifier)
	}
}

// TriggerGenerationForAllSizes 为所有配置尺寸生成缩略图
func (s *ThumbnailService) TriggerGenerationForAllSizes(image *models.Image) {
	ctx := context.Background()

	settings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		log.Printf("[Thumbnail] Failed to get settings: %v", err)
		return
	}

	for _, size := range settings.Sizes {
		s.TriggerGeneration(image, size.Width)
	}
}

// EnsureThumbnail 确保缩略图存在，如果不存在则触发生成
// 返回 true 表示缩略图已存在，false 表示已触发生成
func (s *ThumbnailService) EnsureThumbnail(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, bool, error) {
	result, err := s.GetThumbnail(ctx, image, width)
	if err != nil {
		return nil, false, err
	}

	if result != nil {
		return result, true, nil
	}

	// 缩略图不存在，触发生成
	s.TriggerGeneration(image, width)
	return nil, false, nil
}

// GenerateThumbnailSync 同步生成缩略图
func (s *ThumbnailService) GenerateThumbnailSync(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, error) {
	// 读取原图数据
	imageData, err := s.getImageData(ctx, image.Identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to get image data: %w", err)
	}

	// 生成缩略图
	thumbnailData, height, err := s.resizeImage(imageData, width)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	// 生成存储标识
	thumbnailIdentifier := s.GenerateThumbnailIdentifier(image.Identifier, width)

	// 存储缩略图
	if err := s.storage.SaveWithContext(ctx, thumbnailIdentifier, bytes.NewReader(thumbnailData)); err != nil {
		return nil, fmt.Errorf("failed to store thumbnail: %w", err)
	}

	return &ThumbnailResult{
		Format:     models.GetThumbnailFormat(width),
		Identifier: thumbnailIdentifier,
		Width:      width,
		Height:     height,
		MIMEType:   "image/jpeg",
	}, nil
}

// getImageData 获取图片数据
func (s *ThumbnailService) getImageData(ctx context.Context, identifier string) ([]byte, error) {
	reader, err := s.storage.GetWithContext(ctx, identifier)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// resizeImage 调整图片尺寸
func (s *ThumbnailService) resizeImage(data []byte, width int) ([]byte, int, error) {
	return data, 0, fmt.Errorf("resize not implemented yet")
}

// GenerateThumbnailIdentifier 生成缩略图存储标识
func (s *ThumbnailService) GenerateThumbnailIdentifier(originalIdentifier string, width int) string {
	// images/abc.png -> thumbnails/abc_300.webp
	return fmt.Sprintf("thumbnails/%s_%d.webp", originalIdentifier, width)
}

// getMIMETypeFromFormat 根据格式获取 MIME 类型
func (s *ThumbnailService) getMIMETypeFromFormat(format string) string {
	// 缩略图使用 WebP 格式
	return "image/webp"
}

// GetThumbnailURL 获取缩略图 URL
func (s *ThumbnailService) GetThumbnailURL(identifier string, width int) string {
	return s.GenerateThumbnailIdentifier(identifier, width)
}
