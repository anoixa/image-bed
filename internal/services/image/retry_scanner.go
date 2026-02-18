package image

import (
	"log"
	"time"

	"github.com/anoixa/image-bed/database/repo/images"
)

// RetryScanner 重试扫描器
type RetryScanner struct {
	variantRepo images.VariantRepository
	converter   *Converter
	interval    time.Duration
	batchSize   int
	stopCh      chan struct{}
}

// NewRetryScanner 创建扫描器
func NewRetryScanner(repo images.VariantRepository, converter *Converter, interval time.Duration) *RetryScanner {
	return &RetryScanner{
		variantRepo: repo,
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
	log.Printf("[RetryScanner] Started with interval %v", s.interval)
}

// Stop 停止扫描器
func (s *RetryScanner) Stop() {
	close(s.stopCh)
}

// scanAndRetry 扫描并重试
func (s *RetryScanner) scanAndRetry() {
	now := time.Now()

	// 查询可重试的变体
	variants, err := s.variantRepo.GetRetryableVariants(now, s.batchSize)
	if err != nil {
		log.Printf("[RetryScanner] Failed to get retryable variants: %v", err)
		return
	}

	if len(variants) == 0 {
		return
	}

	log.Printf("[RetryScanner] Found %d retryable variants", len(variants))

	for _, variant := range variants {
		log.Printf("[RetryScanner] Processing variant %d: status=%s, retry_count=%d",
			variant.ID, variant.Status, variant.RetryCount)

		// 使用 ResetForRetry: failed → pending，同时增加 retry_count 和设置 next_retry_at
		err := s.variantRepo.ResetForRetry(variant.ID, s.interval)
		if err != nil {
			log.Printf("[RetryScanner] ResetForRetry failed for variant %d: %v", variant.ID, err)
			continue
		}
		log.Printf("[RetryScanner] ResetForRetry success: variant %d status changed from failed to pending, retry_count incremented", variant.ID)

		// 获取图片信息
		img, err := s.variantRepo.GetImageByID(variant.ImageID)
		if err != nil {
			log.Printf("[RetryScanner] Failed to get image %d: %v", variant.ImageID, err)
			continue
		}

		// 触发转换
		log.Printf("[RetryScanner] Triggering conversion for variant %d (image: %s)",
			variant.ID, img.Identifier)
		s.converter.TriggerWebPConversion(img)
	}
}

// StartRetryScanner 便捷函数：创建并启动扫描器
func StartRetryScanner(repo images.VariantRepository, converter *Converter, interval time.Duration) *RetryScanner {
	scanner := NewRetryScanner(repo, converter, interval)
	scanner.Start()
	return scanner
}
