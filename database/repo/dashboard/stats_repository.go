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

	baseQuery := db.Model(&models.Image{}).Where("deleted_at IS NULL")
	if userID != nil {
		baseQuery = baseQuery.Where("user_id = ?", *userID)
	}

	// 今日
	if err := baseQuery.Session(&gorm.Session{}).
		Where("created_at >= ? AND created_at < ?", todayStart, todayEnd).
		Count(&stats.Today).Error; err != nil {
		return nil, fmt.Errorf("failed to count today images: %w", err)
	}

	// 昨日
	if err := baseQuery.Session(&gorm.Session{}).
		Where("created_at >= ? AND created_at < ?", yesterdayStart, yesterdayEnd).
		Count(&stats.Yesterday).Error; err != nil {
		return nil, fmt.Errorf("failed to count yesterday images: %w", err)
	}

	// 本周
	if err := baseQuery.Session(&gorm.Session{}).
		Where("created_at >= ? AND created_at < ?", weekStart, weekEnd).
		Count(&stats.ThisWeek).Error; err != nil {
		return nil, fmt.Errorf("failed to count this week images: %w", err)
	}

	// 本月
	if err := baseQuery.Session(&gorm.Session{}).
		Where("created_at >= ? AND created_at < ?", monthStart, monthEnd).
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

type dailyStatRow struct {
	Date  string `gorm:"column:date"`
	Count int64  `gorm:"column:count"`
}

// GetDailyStats 获取近 N 天每日统计
func (r *Repository) GetDailyStats(ctx context.Context, days int, userID *uint) ([]DailyStat, error) {
	var rows []dailyStatRow

	// 计算起始时间（N天前的零点）
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -days)

	statsRepoLog.Debugf("Querying from %s, days=%d", startDate.Format("2006-01-02"), days)

	dateExpr := dailyDateExpr(r.db)
	query := r.db.WithContext(ctx).Table("images").
		Select(dateExpr+" as date, COUNT(*) as count").
		Where("created_at >= ? AND deleted_at IS NULL", startDate)
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}

	err := query.
		Group(dateExpr).
		Order("date").
		Scan(&rows).Error

	if err != nil {
		statsRepoLog.Debugf("Error: %v", err)
		return nil, err
	}

	stats, err := parseDailyStatRows(rows, now.Location())
	if err != nil {
		statsRepoLog.Debugf("Error parsing daily stats: %v", err)
		return nil, err
	}

	statsRepoLog.Debugf("Found %d records", len(stats))
	for _, s := range stats {
		statsRepoLog.Debugf("Result: date=%s, count=%d", s.Date.Format("2006-01-02"), s.Count)
	}

	return stats, nil
}

func dailyDateExpr(db *gorm.DB) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return "TO_CHAR(created_at, 'YYYY-MM-DD')"
	}
	return "DATE(created_at)"
}

func parseDailyStatRows(rows []dailyStatRow, loc *time.Location) ([]DailyStat, error) {
	if loc == nil {
		loc = time.Local
	}

	stats := make([]DailyStat, 0, len(rows))
	for _, row := range rows {
		date, err := time.ParseInLocation("2006-01-02", row.Date, loc)
		if err != nil {
			return nil, fmt.Errorf("parse daily stat date %q: %w", row.Date, err)
		}
		stats = append(stats, DailyStat{
			Date:  date,
			Count: row.Count,
		})
	}

	return stats, nil
}
