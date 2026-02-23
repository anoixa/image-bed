package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"gorm.io/gorm"
)

// ThumbnailResult 缩略图结果
type ThumbnailResult struct {
	Format      string
	Identifier  string
	StoragePath string
	Width       int
	Height      int
	MIMEType    string
}

// ThumbnailService 缩略图服务
type ThumbnailService struct {
	variantRepo   *images.VariantRepository
	imageRepo     *images.Repository
	configManager *config.Manager
	storage       storage.Provider
	converter     *Converter
	pathGenerator *generator.PathGenerator
}

// NewThumbnailService 创建缩略图服务
func NewThumbnailService(
	variantRepo *images.VariantRepository,
	imageRepo *images.Repository,
	cm *config.Manager,
	storage storage.Provider,
	converter *Converter,
) *ThumbnailService {
	return &ThumbnailService{
		variantRepo:   variantRepo,
		imageRepo:     imageRepo,
		configManager: cm,
		storage:       storage,
		converter:     converter,
		pathGenerator: generator.NewPathGenerator(),
	}
}

// GetThumbnail 获取缩略图信息
// 如果缩略图不存在，返回 nil，调用方需要触发生成
func (s *ThumbnailService) GetThumbnail(ctx context.Context, image *models.Image, width int) (*ThumbnailResult, error) {
	format := formatThumbnailSize(width)

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
		Format:      format,
		Identifier:  variant.Identifier,
		StoragePath: variant.StoragePath,
		Width:       variant.Width,
		Height:      variant.Height,
		MIMEType:    s.getMIMETypeFromFormat(format),
	}, nil
}

// TriggerGeneration 触发缩略图生成
func (s *ThumbnailService) TriggerGeneration(image *models.Image, width int) {
	ctx := context.Background()

	// 读取配置，失败时使用默认配置
	settings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[Thumbnail] Failed to get settings, using defaults: %v", err)
		settings = config.DefaultThumbnailSettings()
	}

	// 检查图片
	if !settings.Enabled {
		utils.LogIfDevf("[Thumbnail] Thumbnail generation disabled")
		return
	}
	if !isValidThumbnailWidth(width, settings.Sizes) {
		utils.LogIfDevf("[Thumbnail] Invalid thumbnail width: %d", width)
		return
	}

	format := formatThumbnailSize(width)

	// 创建或获取变体记录
	variant, err := s.variantRepo.UpsertPending(image.ID, format)
	if err != nil {
		utils.LogIfDevf("[Thumbnail] Failed to upsert variant: %v", err)
		return
	}

	if variant.Status != models.VariantStatusPending {
		utils.LogIfDevf("[Thumbnail] Variant %d status=%s, skip submission", variant.ID, variant.Status)
		return
	}

	if variant.RetryCount >= settings.MaxRetries {
		utils.LogIfDevf("[Thumbnail] Variant %d reached max retries", variant.ID)
		return
	}

	thumbIDs := s.GenerateThumbnailIdentifiers(image.StoragePath, width)

	// 提交任务
	pool := worker.GetGlobalPool()
	if pool == nil {
		return
	}

	ok := pool.Submit(func() {
		task := &worker.ThumbnailTask{
			VariantID:        variant.ID,
			ImageID:          image.ID,
			SourcePath:       image.StoragePath,
			TargetPath:       thumbIDs.StoragePath,
			TargetIdentifier: thumbIDs.Identifier,
			TargetWidth:      width,
			ConfigManager:    s.configManager,
			VariantRepo:      s.variantRepo,
			Storage:          s.storage,
		}
		task.Execute()
	})

	if !ok {
		utils.LogIfDevf("[Thumbnail] Failed to submit task for %s", image.Identifier)
	}
}

// TriggerGenerationForAllSizes 为所有配置尺寸生成缩略图
func (s *ThumbnailService) TriggerGenerationForAllSizes(image *models.Image) {
	ctx := context.Background()

	settings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		utils.LogIfDevf("[Thumbnail] Failed to get settings: %v", err)
		return
	}

	for _, size := range settings.Sizes {
		s.TriggerGeneration(image, size.Width)
	}
}

// EnsureThumbnail 确保缩略图存在，如果不存在则触发生成
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
	imageData, err := s.getImageData(ctx, image.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get image data: %w", err)
	}

	// 生成缩略图
	thumbnailData, height, err := s.resizeImage(imageData, width)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}
	thumbIDs := s.GenerateThumbnailIdentifiers(image.StoragePath, width)

	if err := s.storage.SaveWithContext(ctx, thumbIDs.StoragePath, bytes.NewReader(thumbnailData)); err != nil {
		return nil, fmt.Errorf("failed to store thumbnail: %w", err)
	}

	return &ThumbnailResult{
		Format:      formatThumbnailSize(width),
		Identifier:  thumbIDs.Identifier,
		StoragePath: thumbIDs.StoragePath,
		Width:       width,
		Height:      height,
		MIMEType:    "image/webp",
	}, nil
}

// getImageData 获取图片数据
func (s *ThumbnailService) getImageData(ctx context.Context, storagePath string) ([]byte, error) {
	reader, err := s.storage.GetWithContext(ctx, storagePath)
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

// GenerateThumbnailIdentifiers 生成缩略图的 identifier 和 storage_path
func (s *ThumbnailService) GenerateThumbnailIdentifiers(originalStoragePath string, width int) generator.StorageIdentifiers {
	return s.pathGenerator.GenerateThumbnailIdentifiers(originalStoragePath, width)
}

// getMIMETypeFromFormat 根据格式获取 MIME 类型
func (s *ThumbnailService) getMIMETypeFromFormat(format string) string {
	// 缩略图使用 WebP 格式
	return "image/webp"
}

// GetThumbnailURL 获取缩略图 URL
func (s *ThumbnailService) GetThumbnailURL(originalStoragePath string, width int) string {
	ids := s.GenerateThumbnailIdentifiers(originalStoragePath, width)
	return ids.StoragePath
}

// formatThumbnailSize 生成缩略图格式标识
func formatThumbnailSize(width int) string {
	return fmt.Sprintf("thumbnail_%d", width)
}

// isValidThumbnailWidth 检查缩略图宽度是否有效
func isValidThumbnailWidth(width int, sizes []models.ThumbnailSize) bool {
	for _, size := range sizes {
		if size.Width == width {
			return true
		}
	}
	return false
}

// ThumbnailScanner 缩略图预生成扫描器
type ThumbnailScanner struct {
	db            *gorm.DB
	configManager *config.Manager
	thumbnailSvc  *ThumbnailService
	ticker        *time.Ticker
	stopChan      chan struct{}
	isRunning     bool
}

// NewThumbnailScanner 创建缩略图扫描器
func NewThumbnailScanner(
	db *gorm.DB,
	configManager *config.Manager,
	thumbnailSvc *ThumbnailService,
) *ThumbnailScanner {
	return &ThumbnailScanner{
		db:            db,
		configManager: configManager,
		thumbnailSvc:  thumbnailSvc,
		stopChan:      make(chan struct{}),
	}
}

// Start 启动扫描器
func (s *ThumbnailScanner) Start() error {
	settings, err := s.configManager.GetThumbnailScannerSettings()
	if err != nil {
		// 获取失败时使用默认配置
		settings = config.GetDefaultThumbnailScannerSettings()
	}

	if !settings.Enabled {
		return nil
	}

	if s.isRunning {
		return nil
	}

	if s.thumbnailSvc == nil {
		return fmt.Errorf("thumbnailSvc is nil")
	}

	s.isRunning = true
	s.ticker = time.NewTicker(settings.Interval)

	go s.runOnce()

	go s.runLoop()

	return nil
}

// Stop 停止扫描器
func (s *ThumbnailScanner) Stop() {
	if !s.isRunning {
		return
	}

	s.isRunning = false
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.stopChan)
}

// IsRunning 检查是否运行中
func (s *ThumbnailScanner) IsRunning() bool {
	return s.isRunning
}

// runOnce 立即执行一次扫描
func (s *ThumbnailScanner) runOnce() {

	settings, err := s.configManager.GetThumbnailScannerSettings()
	if err != nil {
		// 获取失败时使用默认配置
		settings = config.GetDefaultThumbnailScannerSettings()
	}

	if !settings.Enabled {
		return
	}

	// 获取需要生成缩略图的图片
	images, err := s.getImagesNeedingThumbnails(settings.BatchSize)
	if err != nil {
		utils.LogIfDevf("[ThumbnailScanner] Failed to get images: %v", err)
		return
	}

	// 为每个图片生成缩略图
	for _, img := range images {
		select {
		case <-s.stopChan:
			return
		default:
			s.thumbnailSvc.TriggerGenerationForAllSizes(img)
		}
	}
}

// runLoop 定时循环
func (s *ThumbnailScanner) runLoop() {
	for {
		select {
		case <-s.ticker.C:
			s.runOnce()
		case <-s.stopChan:
			return
		}
	}
}

// getImagesNeedingThumbnails 获取需要生成缩略图的图片
func (s *ThumbnailScanner) getImagesNeedingThumbnails(limit int) ([]*models.Image, error) {
	ctx := context.Background()
	settings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		return nil, err
	}

	// 获取已启用缩略图但未完成的图片
	var images []*models.Image
	err = s.db.Where("variant_status IN ?", []models.ImageVariantStatus{
		models.ImageVariantStatusNone,
		models.ImageVariantStatusProcessing,
	}).Limit(limit).Find(&images).Error

	if err != nil {
		return nil, err
	}

	// 过滤掉不需要生成缩略图的图片
	var result []*models.Image
	for _, img := range images {
		if settings.Enabled && img.FileSize > 0 {
			result = append(result, img)
		}
	}

	return result, nil
}
