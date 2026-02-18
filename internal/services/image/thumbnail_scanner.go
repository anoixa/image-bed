package image

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/services/config"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/async"

	"gorm.io/gorm"
)

// ThumbnailScanner 缩略图预生成扫描器
type ThumbnailScanner struct {
	db            *gorm.DB
	configManager *config.Manager
	worker        *async.WorkerPool
	thumbnailSvc  *ThumbnailService
	ticker        *time.Ticker
	stopChan      chan struct{}
	isRunning     bool
}

// NewThumbnailScanner 创建缩略图扫描器
func NewThumbnailScanner(
	db *gorm.DB,
	configManager *config.Manager,
	worker *async.WorkerPool,
	thumbnailSvc *ThumbnailService,
) *ThumbnailScanner {
	return &ThumbnailScanner{
		db:            db,
		configManager: configManager,
		worker:        worker,
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
	
	// 检查依赖
	if s.worker == nil {
		return fmt.Errorf("worker is nil")
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
		task := &async.ThumbnailTask{
			VariantID:        variant.ID,
			ImageID:          image.ID,
			SourceIdentifier: image.Identifier,
			TargetIdentifier: s.thumbnailSvc.GenerateThumbnailIdentifier(image.Identifier, targetWidth),
			TargetWidth:      targetWidth,
			ConfigManager:    s.configManager,
			VariantRepo:      s.thumbnailSvc.variantRepo,
			Storage:          s.thumbnailSvc.storage,
		}

		ok := s.worker.SubmitBlocking(task, 5*time.Second)
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
