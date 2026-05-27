package tempfiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
)

const (
	DefaultSweepInterval = 10 * time.Minute
	DefaultOrphanMaxAge  = time.Hour
)

var knownTempPrefixes = []string{
	"upload-stream-",
	"variant-output-",
	"pipeline-proc-",
	"webdav-get-",
}

var sweeperLog = utils.ForModule("TempSweeper")

type SweepStats struct {
	Checked int
	Deleted int
}

func StartSweeper(ctx context.Context) {
	go func() {
		sweeperLog.Infof("Started (dir=%s, interval=%s, max_age=%s)", config.TempDir, DefaultSweepInterval, DefaultOrphanMaxAge)
		runSweep(ctx)

		ticker := time.NewTicker(DefaultSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				sweeperLog.Infof("Stopped")
				return
			case <-ticker.C:
				runSweep(ctx)
			}
		}
	}()
}

func runSweep(ctx context.Context) {
	stats, err := SweepOrphans(ctx, config.TempDir, DefaultOrphanMaxAge, time.Now())
	if err != nil {
		sweeperLog.Warnf("Failed to sweep temp files: %v", err)
		return
	}
	if stats.Deleted > 0 {
		sweeperLog.Infof("Deleted %d orphan temp files after checking %d files", stats.Deleted, stats.Checked)
	}
}

func SweepOrphans(ctx context.Context, dir string, olderThan time.Duration, now time.Time) (SweepStats, error) {
	var stats SweepStats
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, err
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		if entry.IsDir() || !isKnownTempFile(entry.Name()) {
			continue
		}
		stats.Checked++

		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return stats, err
		}
		if now.Sub(info.ModTime()) <= olderThan {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return stats, err
		}
		stats.Deleted++
	}

	return stats, nil
}

func isKnownTempFile(name string) bool {
	for _, prefix := range knownTempPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
