package image

import (
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
)

// ==================== RetryScanner 重试扫描器 ====================

// RetryScanner 重试扫描器
type RetryScanner struct {
	variantRepo *images.VariantRepository
	imageRepo   *images.Repository
	converter   *Converter
	interval    time.Duration
	batchSize   int
	stopCh      chan struct{}
}

// NewRetryScanner 创建重试扫描器
func NewRetryScanner(variantRepo *images.VariantRepository, imageRepo *images.Repository, converter *Converter, interval time.Duration) *RetryScanner {
	return &RetryScanner{
		variantRepo: variantRepo,
		imageRepo:   imageRepo,
		converter:   converter,
		interval:    interval,
		batchSize:   100,
		stopCh:      make(chan struct{}),
	}
}

// Start 启动扫描器
func (s *RetryScanner) Start() {
	ticker := time.NewTicker(s.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.scanAndRetry()
			case <-s.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
	utils.LogIfDevf("[RetryScanner] Started with interval %v", s.interval)
}

// Stop 停止扫描器
func (s *RetryScanner) Stop() {
	close(s.stopCh)
}

// scanAndRetry 扫描并重试
func (s *RetryScanner) scanAndRetry() {
	now := time.Now()

	// 首先扫描 VariantStatus 为 Failed 的图片
	if s.imageRepo != nil {
		failedImages, err := s.imageRepo.GetImagesByVariantStatus(
			[]models.ImageVariantStatus{models.ImageVariantStatusFailed},
			s.batchSize,
		)
		if err != nil {
			utils.LogIfDevf("[RetryScanner] Failed to get failed images: %v", err)
		} else if len(failedImages) > 0 {
			utils.LogIfDevf("[RetryScanner] Found %d images with failed variant status", len(failedImages))
			for _, img := range failedImages {
				s.converter.TriggerWebPConversion(img)
			}
		}
	}

	// 查询可重试的变体
	variants, err := s.variantRepo.GetRetryableVariants(now, s.batchSize)
	if err != nil {
		utils.LogIfDevf("[RetryScanner] Failed to get retryable variants: %v", err)
		return
	}

	if len(variants) == 0 {
		return
	}

	utils.LogIfDevf("[RetryScanner] Found %d retryable variants", len(variants))

	for _, variant := range variants {
		utils.LogIfDevf("[RetryScanner] Processing variant %d: status=%s, retry_count=%d",
			variant.ID, variant.Status, variant.RetryCount)

		// 使用 ResetForRetry: failed → pending，同时增加 retry_count 和设置 next_retry_at
		err := s.variantRepo.ResetForRetry(variant.ID, s.interval)
		if err != nil {
			utils.LogIfDevf("[RetryScanner] ResetForRetry failed for variant %d: %v", variant.ID, err)
			continue
		}
		utils.LogIfDevf("[RetryScanner] ResetForRetry success: variant %d status changed from failed to pending, retry_count incremented", variant.ID)

		// 获取图片信息
		img, err := s.variantRepo.GetImageByID(variant.ImageID)
		if err != nil {
			utils.LogIfDevf("[RetryScanner] Failed to get image %d: %v", variant.ImageID, err)
			continue
		}

		// 触发转换
		utils.LogIfDevf("[RetryScanner] Triggering conversion for variant %d (image: %s)",
			variant.ID, img.Identifier)
		s.converter.TriggerWebPConversion(img)
	}
}

// StartRetryScanner 创建并启动重试扫描器
func StartRetryScanner(variantRepo *images.VariantRepository, imageRepo *images.Repository, converter *Converter, interval time.Duration) *RetryScanner {
	scanner := NewRetryScanner(variantRepo, imageRepo, converter, interval)
	scanner.Start()
	return scanner
}

// ThumbnailServiceAccessor 缩略图
type ThumbnailServiceAccessor interface {
	TriggerGeneration(image *models.Image, width int)
}

// OrphanScanner 孤儿任务扫描器
type OrphanScanner struct {
	variantRepo      *images.VariantRepository
	converter        *Converter
	thumbnailService ThumbnailServiceAccessor
	interval         time.Duration
	threshold        time.Duration // 超过此时间视为孤儿任务
	batchSize        int
	stopCh           chan struct{}
}

// NewOrphanScanner 创建孤儿任务扫描器
// threshold: 超过多长时间视为孤儿任务（如 10 分钟）
// interval: 扫描间隔（如 5 分钟）
func NewOrphanScanner(repo *images.VariantRepository, converter *Converter, thumbnailService ThumbnailServiceAccessor, threshold, interval time.Duration) *OrphanScanner {
	return &OrphanScanner{
		variantRepo:      repo,
		converter:        converter,
		thumbnailService: thumbnailService,
		interval:         interval,
		threshold:        threshold,
		batchSize:        100,
		stopCh:           make(chan struct{}),
	}
}

// Start 启动扫描器
func (s *OrphanScanner) Start() {
	ticker := time.NewTicker(s.interval)
	go func() {
		// 启动时立即执行一次扫描
		s.scan()

		for {
			select {
			case <-ticker.C:
				s.scan()
			case <-s.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
	utils.LogIfDevf("[OrphanScanner] Started with threshold %v, interval %v", s.threshold, s.interval)
}

// Stop 停止扫描器
func (s *OrphanScanner) Stop() {
	close(s.stopCh)
}

// scan 扫描并处理孤儿任务
func (s *OrphanScanner) scan() {
	// 查询长时间处于 processing 状态的任务
	variants, err := s.variantRepo.GetOrphanVariants(s.threshold, s.batchSize)
	if err != nil {
		utils.LogIfDevf("[OrphanScanner] Failed to get orphan variants: %v", err)
		return
	}

	if len(variants) == 0 {
		return
	}

	utils.LogIfDevf("[OrphanScanner] Found %d orphan variants", len(variants))

	for _, variant := range variants {
		s.processOrphanVariant(variant)
	}
}

// processOrphanVariant 处理单个孤儿任务
func (s *OrphanScanner) processOrphanVariant(variant models.ImageVariant) {
	utils.LogIfDevf("[OrphanScanner] Processing orphan variant %d: status=%s, updated_at=%v",
		variant.ID, variant.Status, variant.UpdatedAt)

	// 重置为 pending 状态
	if err := s.variantRepo.ResetProcessingToPending(variant.ID); err != nil {
		utils.LogIfDevf("[OrphanScanner] Failed to reset variant %d: %v", variant.ID, err)
		return
	}
	utils.LogIfDevf("[OrphanScanner] Reset variant %d from processing to pending", variant.ID)

	// 获取图片信息
	img, err := s.variantRepo.GetImageByID(variant.ImageID)
	if err != nil {
		utils.LogIfDevf("[OrphanScanner] Failed to get image %d: %v", variant.ImageID, err)
		return
	}

	// 判断是 WebP 转换还是缩略图生成
	if width, ok := models.ParseThumbnailSize(variant.Format); ok {
		utils.LogIfDevf("[OrphanScanner] Triggering thumbnail generation for variant %d", variant.ID)
		if ok && width > 0 && s.thumbnailService != nil {
			s.thumbnailService.TriggerGeneration(img, width)
		}
	} else {
		// WebP 转换任务
		utils.LogIfDevf("[OrphanScanner] Triggering WebP conversion for variant %d", variant.ID)
		s.converter.TriggerWebPConversion(img)
	}
}

// StartOrphanScanner 创建并启动孤儿任务扫描器
func StartOrphanScanner(repo *images.VariantRepository, converter *Converter, thumbnailService ThumbnailServiceAccessor, threshold, interval time.Duration) *OrphanScanner {
	scanner := NewOrphanScanner(repo, converter, thumbnailService, threshold, interval)
	scanner.Start()
	return scanner
}
