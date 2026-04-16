package worker

import (
	"context"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

const sweeperInterval = 5 * time.Minute
const staleThreshold = 15 * time.Minute

// StartVariantSweeper runs a background goroutine that periodically resets
// stale processing variants back to pending so they can be retried.
func StartVariantSweeper(ctx context.Context, db *gorm.DB) {
	go func() {
		variantRepo := images.NewVariantRepository(db)
		ticker := time.NewTicker(sweeperInterval)
		defer ticker.Stop()

		utils.Infof("[Sweeper] Started (interval=%s, stale threshold=%s)", sweeperInterval, staleThreshold)

		for {
			select {
			case <-ctx.Done():
				utils.Infof("[Sweeper] Stopped")
				return
			case <-ticker.C:
				sweepOnce(ctx, variantRepo, db)
			}
		}
	}()
}

func sweepOnce(ctx context.Context, variantRepo *images.VariantRepository, db *gorm.DB) {
	reset, err := variantRepo.ResetStaleProcessing(staleThreshold)
	if err != nil {
		utils.Warnf("[Sweeper] Failed to reset stale variants: %v", err)
		return
	}
	if reset > 0 {
		utils.Infof("[Sweeper] Reset %d stale processing variants to pending", reset)
	}

	// Also reset images whose variant_status is stuck in processing.
	result := db.Model(&models.Image{}).
		Where("variant_status = ? AND updated_at < ?", models.ImageVariantStatusProcessing, time.Now().Add(-staleThreshold)).
		Update("variant_status", models.ImageVariantStatusNone)
	if result.Error != nil {
		utils.Warnf("[Sweeper] Failed to reset stale image variant_status: %v", result.Error)
	} else if result.RowsAffected > 0 {
		utils.Infof("[Sweeper] Reset %d stale processing images to none", result.RowsAffected)
	}
}
