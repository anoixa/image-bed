package image

import (
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
)

// ThumbnailServiceAccessor 缩略图服务访问接口
type ThumbnailServiceAccessor interface {
	TriggerGeneration(image *models.Image, width int)
}

// OrphanScanner 孤儿任务扫描器
// 用于检测长时间处于 processing 状态的任务（进程崩溃导致的孤儿任务）
type OrphanScanner struct {
	variantRepo      images.VariantRepository
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
func NewOrphanScanner(repo images.VariantRepository, converter *Converter, thumbnailService ThumbnailServiceAccessor, threshold, interval time.Duration) *OrphanScanner {
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
	if models.IsThumbnailFormat(variant.Format) {
		utils.LogIfDevf("[OrphanScanner] Triggering thumbnail generation for variant %d", variant.ID)
		width, ok := models.ParseThumbnailWidth(variant.Format)
		if ok && width > 0 && s.thumbnailService != nil {
			s.thumbnailService.TriggerGeneration(img, width)
		}
	} else {
		// WebP 转换任务
		utils.LogIfDevf("[OrphanScanner] Triggering WebP conversion for variant %d", variant.ID)
		s.converter.TriggerWebPConversion(img)
	}
}

// StartOrphanScanner 创建并启动扫描器
func StartOrphanScanner(repo images.VariantRepository, converter *Converter, thumbnailService ThumbnailServiceAccessor, threshold, interval time.Duration) *OrphanScanner {
	scanner := NewOrphanScanner(repo, converter, thumbnailService, threshold, interval)
	scanner.Start()
	return scanner
}
