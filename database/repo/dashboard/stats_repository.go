package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils"
	"gorm.io/gorm"
)

var statsRepoLog = utils.ForModule("StatsRepository")

// Repository Dashboard 统计仓库
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的 Dashboard 统计仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// WithContext 返回带上下文的仓库副本
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return &Repository{db: r.db.WithContext(ctx)}
}

// OverviewStats 概览统计
type OverviewStats struct {
	ImageTotal   int64
	AlbumTotal   int64
	UserTotal    int64
	StorageTotal int64
}

// GetOverviewStats 获取概览统计
func (r *Repository) GetOverviewStats(ctx context.Context) (*OverviewStats, error) {
	var result OverviewStats

	db := r.db.WithContext(ctx)

	// 图片总数和存储大小
	err := db.Model(&models.Image{}).
		Select("COUNT(*) as image_total, COALESCE(SUM(file_size), 0) as storage_total").
		Where("deleted_at IS NULL").
		Scan(&result).Error
	if err != nil {
		return nil, err
	}

	// 相册数量
	if err := db.Model(&models.Album{}).
		Where("deleted_at IS NULL").
		Select("COUNT(*)").
		Scan(&result.AlbumTotal).Error; err != nil {
		return nil, fmt.Errorf("failed to count albums: %w", err)
	}

	// 用户固定为1（单用户系统）
	result.UserTotal = 1

	return &result, nil
}

// ImageTimeStats 图片时间维度统计
type ImageTimeStats struct {
	Today     int64
	Yesterday int64
	ThisWeek  int64
	ThisMonth int64
}

// GetImageTimeStats 获取图片时间维度统计
func (r *Repository) GetImageTimeStats(ctx context.Context) (*ImageTimeStats, error) {
	var stats ImageTimeStats
	now := time.Now()
	db := r.db.WithContext(ctx)

	// 计算今日时间范围
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.Add(24 * time.Hour)

	// 计算昨日时间范围
	yesterdayStart := todayStart.Add(-24 * time.Hour)
	yesterdayEnd := todayStart

	// 计算本周时间范围 (周一到周日)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		Add(-time.Duration(weekday-1) * 24 * time.Hour)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	// 计算本月时间范围
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	// 今日
	if err := db.Model(&models.Image{}).
		Where("created_at >= ? AND created_at < ? AND deleted_at IS NULL", todayStart, todayEnd).
		Count(&stats.Today).Error; err != nil {
		return nil, fmt.Errorf("failed to count today images: %w", err)
	}

	// 昨日
	if err := db.Model(&models.Image{}).
		Where("created_at >= ? AND created_at < ? AND deleted_at IS NULL", yesterdayStart, yesterdayEnd).
		Count(&stats.Yesterday).Error; err != nil {
		return nil, fmt.Errorf("failed to count yesterday images: %w", err)
	}

	// 本周
	if err := db.Model(&models.Image{}).
		Where("created_at >= ? AND created_at < ? AND deleted_at IS NULL", weekStart, weekEnd).
		Count(&stats.ThisWeek).Error; err != nil {
		return nil, fmt.Errorf("failed to count this week images: %w", err)
	}

	// 本月
	if err := db.Model(&models.Image{}).
		Where("created_at >= ? AND created_at < ? AND deleted_at IS NULL", monthStart, monthEnd).
		Count(&stats.ThisMonth).Error; err != nil {
		return nil, fmt.Errorf("failed to count this month images: %w", err)
	}

	return &stats, nil
}

// StorageStat 存储统计原始数据
type StorageStat struct {
	StorageID   uint
	StorageName string
	Count       int64
	Size        int64
}

// GetStorageStats 获取各存储类型统计
func (r *Repository) GetStorageStats(ctx context.Context) ([]StorageStat, error) {
	var stats []StorageStat

	err := r.db.WithContext(ctx).Table("images i").
		Select("i.storage_config_id as storage_id, sc.name as storage_name, COUNT(*) as count, SUM(i.file_size) as size").
		Joins("LEFT JOIN system_configs sc ON i.storage_config_id = sc.id").
		Where("i.deleted_at IS NULL").
		Group("i.storage_config_id, sc.name").
		Order("size DESC").
		Scan(&stats).Error

	return stats, err
}

// DailyStat 每日统计
type DailyStat struct {
	Date  time.Time
	Count int64
}

// GetDailyStats 获取近 N 天每日统计
func (r *Repository) GetDailyStats(ctx context.Context, days int) ([]DailyStat, error) {
	var stats []DailyStat

	// 计算起始时间（N天前的零点）
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -days)

	statsRepoLog.Debugf("Querying from %s, days=%d", startDate.Format("2006-01-02"), days)

	err := r.db.WithContext(ctx).Table("images").
		Select("DATE(created_at) as date, COUNT(*) as count").
		Where("created_at >= ? AND deleted_at IS NULL", startDate).
		Group("DATE(created_at)").
		Order("date").
		Scan(&stats).Error

	if err != nil {
		statsRepoLog.Debugf("Error: %v", err)
	} else {
		statsRepoLog.Debugf("Found %d records", len(stats))
		for _, s := range stats {
			statsRepoLog.Debugf("Result: date=%s, count=%d", s.Date.Format("2006-01-02"), s.Count)
		}
	}

	return stats, err
}
