package image

import (
	"log"
	"time"

	"github.com/anoixa/image-bed/database/models"
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
		// CAS: failed → pending
		updated, err := s.variantRepo.UpdateStatusCAS(
			variant.ID,
			models.VariantStatusFailed,
			models.VariantStatusPending,
			"",
		)
		if err != nil || !updated {
			continue // 已被其他进程处理
		}

		// 获取图片信息
		img, err := s.variantRepo.GetImageByID(variant.ImageID)
		if err != nil {
			log.Printf("[RetryScanner] Failed to get image %d: %v", variant.ImageID, err)
			continue
		}

		// 触发转换
		s.converter.TriggerWebPConversion(img)
		log.Printf("[RetryScanner] Triggered retry for variant %d (image: %s)",
			variant.ID, img.Identifier)
	}
}

// StartRetryScanner 便捷函数：创建并启动扫描器
func StartRetryScanner(repo images.VariantRepository, converter *Converter, interval time.Duration) *RetryScanner {
	scanner := NewRetryScanner(repo, converter, interval)
	scanner.Start()
	return scanner
}
