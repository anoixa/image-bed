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
func (r *Repository) GetOverviewStats(ctx context.Context, userID *uint) (*OverviewStats, error) {
	var result OverviewStats

	db := r.db.WithContext(ctx)

	imageQuery := db.Model(&models.Image{}).Where("deleted_at IS NULL")
	if userID != nil {
		imageQuery = imageQuery.Where("user_id = ?", *userID)
	}

	err := imageQuery.
		Select("COUNT(*) as image_total, COALESCE(SUM(file_size), 0) as storage_total").
		Scan(&result).Error
	if err != nil {
		return nil, err
	}

	albumQuery := db.Model(&models.Album{}).Where("deleted_at IS NULL")
	if userID != nil {
		albumQuery = albumQuery.Where("user_id = ?", *userID)
	}
	if err := albumQuery.Select("COUNT(*)").Scan(&result.AlbumTotal).Error; err != nil {
		return nil, fmt.Errorf("failed to count albums: %w", err)
	}

	// 普通用户视角不应看到全局用户总数
	if userID == nil {
		if err := db.Model(&models.User{}).Count(&result.UserTotal).Error; err != nil {
			return nil, fmt.Errorf("failed to count users: %w", err)
		}
	} else {
		result.UserTotal = 1
	}

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
func (r *Repository) GetImageTimeStats(ctx context.Context, userID *uint) (*ImageTimeStats, error) {
	var stats ImageTimeStats
	now := time.Now().In(time.Local)

	// 计算今日时间范围
	todayStart := startOfDay(now)
	todayEnd := todayStart.Add(24 * time.Hour)

	// 计算昨日时间范围
	yesterdayStart := todayStart.Add(-24 * time.Hour)
	yesterdayEnd := todayStart

	// 计算本周时间范围 (周一到周日)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekStart := todayStart.Add(-time.Duration(weekday-1) * 24 * time.Hour)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)

	// 计算本月时间范围
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	monthEnd := monthStart.AddDate(0, 1, 0)

	rangeStart := minTime(yesterdayStart, weekStart, monthStart)
	rangeEnd := maxTime(todayEnd, weekEnd, monthEnd)
	createdAtValues, err := r.getCreatedAtValues(ctx, rangeStart, rangeEnd, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get image timestamps: %w", err)
	}

	for _, createdAt := range createdAtValues {
		localCreatedAt := createdAt.In(time.Local)
		if inTimeRange(localCreatedAt, todayStart, todayEnd) {
			stats.Today++
		}
		if inTimeRange(localCreatedAt, yesterdayStart, yesterdayEnd) {
			stats.Yesterday++
		}
		if inTimeRange(localCreatedAt, weekStart, weekEnd) {
			stats.ThisWeek++
		}
		if inTimeRange(localCreatedAt, monthStart, monthEnd) {
			stats.ThisMonth++
		}
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
func (r *Repository) GetStorageStats(ctx context.Context, userID *uint) ([]StorageStat, error) {
	var stats []StorageStat

	query := r.db.WithContext(ctx).Table("images i").
		Select("i.storage_config_id as storage_id, sc.name as storage_name, COUNT(*) as count, SUM(i.file_size) as size").
		Joins("LEFT JOIN system_configs sc ON i.storage_config_id = sc.id").
		Where("i.deleted_at IS NULL")
	if userID != nil {
		query = query.Where("i.user_id = ?", *userID)
	}

	err := query.
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

type createdAtRow struct {
	CreatedAt time.Time `gorm:"column:created_at"`
}

// GetDailyStats 获取近 N 天每日统计
func (r *Repository) GetDailyStats(ctx context.Context, days int, userID *uint) ([]DailyStat, error) {
	if days <= 0 {
		return nil, nil
	}

	now := time.Now().In(time.Local)
	todayStart := startOfDay(now)
	startDate := todayStart.AddDate(0, 0, -(days - 1))
	endDate := todayStart.AddDate(0, 0, 1)

	statsRepoLog.Debugf("Querying from %s, days=%d", startDate.Format("2006-01-02"), days)

	createdAtValues, err := r.getCreatedAtValues(ctx, startDate, endDate, userID)
	if err != nil {
		statsRepoLog.Debugf("Error: %v", err)
		return nil, err
	}

	counts := make(map[string]int64, days)
	for _, createdAt := range createdAtValues {
		localCreatedAt := createdAt.In(time.Local)
		if !inTimeRange(localCreatedAt, startDate, endDate) {
			continue
		}
		counts[localCreatedAt.Format("2006-01-02")]++
	}

	stats := make([]DailyStat, 0, len(counts))
	for i := 0; i < days; i++ {
		date := startDate.AddDate(0, 0, i)
		count := counts[date.Format("2006-01-02")]
		if count == 0 {
			continue
		}
		stats = append(stats, DailyStat{
			Date:  date,
			Count: count,
		})
	}

	statsRepoLog.Debugf("Found %d records", len(stats))
	for _, s := range stats {
		statsRepoLog.Debugf("Result: date=%s, count=%d", s.Date.Format("2006-01-02"), s.Count)
	}

	return stats, nil
}

func (r *Repository) getCreatedAtValues(ctx context.Context, start, end time.Time, userID *uint) ([]time.Time, error) {
	var rows []createdAtRow

	query := r.db.WithContext(ctx).Table("images").
		Select("created_at").
		Where("created_at >= ? AND created_at < ? AND deleted_at IS NULL", start.Add(-24*time.Hour), end.Add(24*time.Hour))
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}
	if err := query.Order("created_at").Scan(&rows).Error; err != nil {
		return nil, err
	}

	values := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.CreatedAt)
	}
	return values, nil
}

func startOfDay(t time.Time) time.Time {
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func inTimeRange(value, start, end time.Time) bool {
	return !value.Before(start) && value.Before(end)
}

func minTime(values ...time.Time) time.Time {
	if len(values) == 0 {
		return time.Time{}
	}
	minValue := values[0]
	for _, value := range values[1:] {
		if value.Before(minValue) {
			minValue = value
		}
	}
	return minValue
}

func maxTime(values ...time.Time) time.Time {
	if len(values) == 0 {
		return time.Time{}
	}
	maxValue := values[0]
	for _, value := range values[1:] {
		if value.After(maxValue) {
			maxValue = value
		}
	}
	return maxValue
}
