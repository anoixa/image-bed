package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

// ==================== ThumbnailService 缩略图服务 ====================

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
	variantRepo   *images.VariantRepository
	configManager *config.Manager
	storage       storage.Provider
	converter     *Converter
}

// NewThumbnailService 创建缩略图服务
func NewThumbnailService(
	repo *images.VariantRepository,
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
		utils.LogIfDevf("[Thumbnail] Failed to get settings, using defaults: %v", err)
		settings = config.DefaultThumbnailSettings()
	}

	// 检查缩略图是否启用
	if !settings.Enabled {
		utils.LogIfDevf("[Thumbnail] Thumbnail generation disabled")
		return
	}

	// 检查是否为有效尺寸
	if !models.IsValidThumbnailWidth(width, settings.Sizes) {
		utils.LogIfDevf("[Thumbnail] Invalid thumbnail width: %d", width)
		return
	}

	format := models.GetThumbnailFormat(width)

	// 创建或获取变体记录
	variant, err := s.variantRepo.UpsertPending(image.ID, format)
	if err != nil {
		utils.LogIfDevf("[Thumbnail] Failed to upsert variant: %v", err)
		return
	}

	// 只有 pending 状态才提交任务
	if variant.Status != models.VariantStatusPending {
		utils.LogIfDevf("[Thumbnail] Variant %d status=%s, skip submission", variant.ID, variant.Status)
		return
	}

	// 检查重试次数
	if variant.RetryCount >= settings.MaxRetries {
		utils.LogIfDevf("[Thumbnail] Variant %d reached max retries", variant.ID)
		return
	}

	// 生成缩略图标识
	thumbnailIdentifier := s.GenerateThumbnailIdentifier(image.Identifier, width)

	// 提交任务
	task := &worker.ThumbnailTask{
		VariantID:        variant.ID,
		ImageID:          image.ID,
		SourceIdentifier: image.Identifier,
		TargetIdentifier: thumbnailIdentifier,
		TargetWidth:      width,
		ConfigManager:    s.configManager,
		VariantRepo:      s.variantRepo,
		Storage:          s.storage,
	}

	if !worker.TrySubmit(task, 3, 100*time.Millisecond) {
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

// ==================== ThumbnailScanner 缩略图扫描器 ====================

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

	// 立即执行一次
	go s.runOnce()

	// 定期执行
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

// runLoop 运行循环
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

// runOnce 执行一次扫描
func (s *ThumbnailScanner) runOnce() {
	settings, err := s.configManager.GetThumbnailScannerSettings()
	if err != nil {
		settings = config.GetDefaultThumbnailScannerSettings()
	}

	if !settings.Enabled {
		return
	}

	ctx := context.Background()
	processed := 0
	skipped := 0
	errors := 0

	// 查询需要处理的图片
	images, err := s.queryImagesForThumbnail(settings)
	if err != nil {
		utils.LogIfDevf("[ThumbnailScanner] Failed to query images: %v", err)
		return
	}

	utils.LogIfDevf("[ThumbnailScanner] Found %d images to process", len(images))

	if len(images) == 0 {
		return
	}

	// 批量检查所有图片的缩略图生成需求（使用单次查询）
	imageTasks, err := s.batchCheckNeedsGeneration(ctx, images, settings)
	if err != nil {
		utils.LogIfDevf("[ThumbnailScanner] Error batch checking generation needs: %v", err)
		return
	}

	utils.LogIfDevf("[ThumbnailScanner] Found %d images that need thumbnail generation", len(imageTasks))

	if len(imageTasks) == 0 {
		utils.LogIfDevf("[ThumbnailScanner] All %d images have complete thumbnails, skipping", len(images))
		return
	}

	// 处理每个需要生成缩略图的图片
	for _, task := range imageTasks {
		select {
		case <-s.stopChan:
			return
		default:
		}

		if len(task.MissingFormats) == 0 {
			skipped++
			continue
		}

		// 提交缩略图生成任务
		if err := s.submitThumbnailTask(ctx, task.Image, task.MissingFormats); err != nil {
			utils.LogIfDevf("[ThumbnailScanner] Error submitting task for image %d: %v", task.Image.ID, err)
			errors++
			continue
		}

		processed++

		// 小延迟避免过载
		time.Sleep(100 * time.Millisecond)
	}

	utils.LogIfDevf("[ThumbnailScanner] Run completed - Processed: %d, Skipped: %d, Errors: %d", processed, skipped, errors)
}

// ImageTask 图片任务信息
type ImageTask struct {
	Image          *models.Image
	MissingFormats []string
}

// batchCheckNeedsGeneration 批量检查哪些图片需要生成缩略图（单次数据库查询）
func (s *ThumbnailScanner) batchCheckNeedsGeneration(ctx context.Context, images []models.Image, settings *config.ThumbnailScannerConfig) ([]ImageTask, error) {
	// 获取缩略图配置
	thumbnailSettings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		// 获取失败，假设所有图片都需要生成
		return s.buildAllTasks(images, thumbnailSettings), nil
	}

	if !thumbnailSettings.Enabled {
		return nil, nil
	}

	// 构建所有需要的格式列表
	formats := make([]string, 0, len(thumbnailSettings.Sizes))
	for _, size := range thumbnailSettings.Sizes {
		formats = append(formats, models.GetThumbnailFormat(size.Width))
	}

	// 构建所有图片ID列表
	imageIDs := make([]uint, 0, len(images))
	for i := range images {
		imageIDs = append(imageIDs, images[i].ID)
	}

	// 批量查询所有变体状态（单次查询）
	variantStatus, err := s.thumbnailSvc.variantRepo.GetMissingThumbnailVariants(imageIDs, formats)
	if err != nil {
		return nil, err
	}

	// 构建任务列表
	var tasks []ImageTask
	for i := range images {
		image := &images[i]
		var missingFormats []string

		for _, size := range thumbnailSettings.Sizes {
			format := models.GetThumbnailFormat(size.Width)
			// 检查变体是否存在且已完成
			if exists, ok := variantStatus[image.ID][format]; !ok || !exists {
				missingFormats = append(missingFormats, format)
			}
		}

		if len(missingFormats) > 0 {
			tasks = append(tasks, ImageTask{
				Image:          image,
				MissingFormats: missingFormats,
			})
		}
	}

	return tasks, nil
}

// buildAllTasks 为所有图片构建任务（当配置获取失败时使用）
func (s *ThumbnailScanner) buildAllTasks(images []models.Image, settings *config.ThumbnailSettings) []ImageTask {
	// 尝试获取默认尺寸
	var sizes []models.ThumbnailSize
	if settings != nil && len(settings.Sizes) > 0 {
		sizes = settings.Sizes
	} else {
		sizes = models.DefaultThumbnailSizes
	}

	var tasks []ImageTask
	for i := range images {
		var missingFormats []string
		for _, size := range sizes {
			missingFormats = append(missingFormats, models.GetThumbnailFormat(size.Width))
		}
		tasks = append(tasks, ImageTask{
			Image:          &images[i],
			MissingFormats: missingFormats,
		})
	}
	return tasks
}

// queryImagesForThumbnail 查询需要生成缩略图的图片
func (s *ThumbnailScanner) queryImagesForThumbnail(settings *config.ThumbnailScannerConfig) ([]models.Image, error) {
	var images []models.Image

	// 构建基础查询
	query := s.db.Model(&models.Image{})

	// 文件大小限制 (bytes)
	if settings.MaxFileSizeMB > 0 {
		maxSize := int64(settings.MaxFileSizeMB) * 1024 * 1024
		query = query.Where("file_size <= ?", maxSize)
	}

	// 时间限制
	if settings.MaxAgeDays > 0 {
		cutoffTime := time.Now().AddDate(0, 0, -settings.MaxAgeDays)
		query = query.Where("created_at >= ?", cutoffTime)
	}

	// 仅公开图片
	if settings.OnlyPublicImages {
		query = query.Where("is_public = ?", true)
	}

	// 按时间倒序，优先处理较新的图片
	err := query.Order("created_at DESC").
		Limit(settings.BatchSize).
		Find(&images).Error

	return images, err
}

// submitThumbnailTask 提交缩略图生成任务
func (s *ThumbnailScanner) submitThumbnailTask(ctx context.Context, image *models.Image, missingFormats []string) error {
	thumbnailSettings, err := s.configManager.GetThumbnailSettings(ctx)
	if err != nil {
		return err
	}

	submitted := 0
	// 只为缺失的尺寸提交任务
	for _, format := range missingFormats {
		// 获取对应的尺寸配置
		var targetWidth int
		for _, size := range thumbnailSettings.Sizes {
			if models.GetThumbnailFormat(size.Width) == format {
				targetWidth = size.Width
				break
			}
		}
		if targetWidth == 0 {
			continue
		}

		// 创建待处理记录（如果存在则返回现有记录）
		variant, err := s.thumbnailSvc.variantRepo.UpsertPending(image.ID, format)
		if err != nil {
			return fmt.Errorf("failed to create variant record: %w", err)
		}

		// 创建缩略图任务
		task := &worker.ThumbnailTask{
			VariantID:        variant.ID,
			ImageID:          image.ID,
			SourceIdentifier: image.Identifier,
			TargetIdentifier: s.thumbnailSvc.GenerateThumbnailIdentifier(image.Identifier, targetWidth),
			TargetWidth:      targetWidth,
			ConfigManager:    s.configManager,
			VariantRepo:      s.thumbnailSvc.variantRepo,
			Storage:          s.thumbnailSvc.storage,
		}

		ok := worker.SubmitBlocking(task, 5*time.Second)
		if !ok {
			utils.LogIfDevf("[ThumbnailScanner] Failed to submit task for variant %d (image %d, format %s)",
				variant.ID, image.ID, format)
		} else {
			submitted++
		}
	}

	utils.LogIfDevf("[ThumbnailScanner] Submitted %d tasks for image %d", submitted, image.ID)
	return nil
}

// TriggerManualScan 手动触发一次扫描
func (s *ThumbnailScanner) TriggerManualScan() error {
	if !s.isRunning {
		return fmt.Errorf("scanner is not running")
	}

	go s.runOnce()
	return nil
}

// GetStatus 获取扫描器状态
func (s *ThumbnailScanner) GetStatus() map[string]interface{} {
	settings, err := s.configManager.GetThumbnailScannerSettings()
	if err != nil {
		settings = config.GetDefaultThumbnailScannerSettings()
	}

	return map[string]interface{}{
		"is_running":    s.isRunning,
		"enabled":       settings.Enabled,
		"interval":      settings.Interval.String(),
		"batch_size":    settings.BatchSize,
		"max_file_size": settings.MaxFileSizeMB,
		"max_age_days":  settings.MaxAgeDays,
		"only_public":   settings.OnlyPublicImages,
	}
}
