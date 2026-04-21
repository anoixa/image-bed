package worker

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
)

const sweeperInterval = 5 * time.Minute
const staleThreshold = 15 * time.Minute
const staleMaxRetries = 3

var sweeperLog = utils.ForModule("Sweeper")

type SweeperStats struct {
	Runs             uint64 `json:"runs"`
	Errors           uint64 `json:"errors"`
	ResetVariants    uint64 `json:"reset_variants"`
	FailedVariants   uint64 `json:"failed_variants"`
	FailedImages     uint64 `json:"failed_images"`
	ResetImages      uint64 `json:"reset_images"`
	Retriggered      uint64 `json:"retriggered"`
	LastRunUnix      int64  `json:"last_run_unix"`
	LastSuccessUnix  int64  `json:"last_success_unix"`
	LastErrorUnix    int64  `json:"last_error_unix"`
	LastErrorMessage string `json:"last_error_message"`
}

var sweeperStats = struct {
	runs            atomic.Uint64
	errors          atomic.Uint64
	resetVariants   atomic.Uint64
	failedVariants  atomic.Uint64
	failedImages    atomic.Uint64
	resetImages     atomic.Uint64
	retriggered     atomic.Uint64
	lastRunUnix     atomic.Int64
	lastSuccessUnix atomic.Int64
	lastErrorUnix   atomic.Int64
	lastError       atomic.Pointer[string]
}{}

// TriggerFunc re-enqueues an image for variant processing.
type TriggerFunc func(image *models.Image)

// StartVariantSweeper runs a background goroutine that periodically resets
// stale processing variants back to pending so they can be retried.
// If triggerFn is non-nil, images with reset variants are re-submitted for processing.
func StartVariantSweeper(ctx context.Context, variantRepo *images.VariantRepository, imageRepo *images.Repository, triggerFn TriggerFunc) {
	go func() {
		ticker := time.NewTicker(sweeperInterval)
		defer ticker.Stop()

		sweeperLog.Infof("Started (interval=%s, stale threshold=%s)", sweeperInterval, staleThreshold)

		for {
			select {
			case <-ctx.Done():
				sweeperLog.Infof("Stopped")
				return
			case <-ticker.C:
				sweepOnce(ctx, variantRepo, imageRepo, triggerFn)
			}
		}
	}()
}

func sweepOnce(_ context.Context, variantRepo *images.VariantRepository, imageRepo *images.Repository, triggerFn TriggerFunc) {
	start := time.Now()
	now := start
	sweeperStats.runs.Add(1)
	sweeperStats.lastRunUnix.Store(now.Unix())
	cutoff := time.Now().Add(-staleThreshold)

	var retriggered uint64

	reset, failed, retriedImageIDs, err := variantRepo.RecoverStaleProcessing(staleThreshold, staleMaxRetries)
	if err != nil {
		recordSweeperError(now, err.Error())
		sweeperLog.Warnf("Failed to reset stale variants: %v", err)
		return
	}
	if reset > 0 {
		sweeperStats.resetVariants.Add(uint64(reset))
	}
	if failed > 0 {
		sweeperStats.failedVariants.Add(uint64(failed))
	}

	// Re-trigger conversion for images with reset variants
	if triggerFn != nil && len(retriedImageIDs) > 0 {
		for _, imageID := range retriedImageIDs {
			img, err := imageRepo.GetImageByID(imageID)
			if err != nil {
				sweeperLog.Warnf("Failed to fetch image %d for re-trigger: %v", imageID, err)
				continue
			}
			triggerFn(img)
			retriggered++
		}
		sweeperStats.retriggered.Add(retriggered)
		sweeperLog.Infof("Re-triggered conversion for %d images", retriggered)
	}

	// Images that are no longer processing and have at least one failed
	// variant should surface as failed rather than silently reverting to none.
	failedRows, err := imageRepo.MarkStaleProcessingAsFailed(cutoff, retriedImageIDs)
	if err != nil {
		recordSweeperError(now, err.Error())
		sweeperLog.Warnf("Failed to mark stale processing images as failed: %v", err)
	} else if failedRows > 0 {
		sweeperStats.failedImages.Add(uint64(failedRows))
	}

	// Remaining stale images without failed variants can return to none and be
	// retriggered on demand.
	resetRows, err := imageRepo.ResetStaleProcessingToNone(cutoff, retriedImageIDs)
	if err != nil {
		recordSweeperError(now, err.Error())
		sweeperLog.Warnf("Failed to reset stale image variant_status: %v", err)
	} else if resetRows > 0 {
		sweeperStats.resetImages.Add(uint64(resetRows))
	}

	sweeperStats.lastSuccessUnix.Store(now.Unix())

	elapsed := time.Since(start)
	sweeperLog.Debugf("Sweep completed in %s: variants_reset=%d, variants_failed=%d, images_failed=%d, images_reset=%d, retriggered=%d",
		elapsed, reset, failed, failedRows, resetRows, retriggered)
}

func recordSweeperError(now time.Time, msg string) {
	sweeperStats.errors.Add(1)
	sweeperStats.lastErrorUnix.Store(now.Unix())
	sweeperStats.lastError.Store(&msg)
}

func GetSweeperStats() SweeperStats {
	stats := SweeperStats{
		Runs:            sweeperStats.runs.Load(),
		Errors:          sweeperStats.errors.Load(),
		ResetVariants:   sweeperStats.resetVariants.Load(),
		FailedVariants:  sweeperStats.failedVariants.Load(),
		FailedImages:    sweeperStats.failedImages.Load(),
		ResetImages:     sweeperStats.resetImages.Load(),
		Retriggered:     sweeperStats.retriggered.Load(),
		LastRunUnix:     sweeperStats.lastRunUnix.Load(),
		LastSuccessUnix: sweeperStats.lastSuccessUnix.Load(),
		LastErrorUnix:   sweeperStats.lastErrorUnix.Load(),
	}
	if lastErr := sweeperStats.lastError.Load(); lastErr != nil {
		stats.LastErrorMessage = *lastErr
	}
	return stats
}
