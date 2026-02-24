package dashboard

import (
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Repository Dashboard 统计仓库
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建新的 Dashboard 统计仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// OverviewStats 概览统计
type OverviewStats struct {
	ImageTotal   int64
	AlbumTotal   int64
	UserTotal    int64
	StorageTotal int64
}

// GetOverviewStats 获取概览统计
func (r *Repository) GetOverviewStats() (*OverviewStats, error) {
	var result OverviewStats

	// 图片总数和存储大小
	err := r.db.Model(&models.Image{}).
		Select("COUNT(*) as image_total, COALESCE(SUM(file_size), 0) as storage_total").
		Where("deleted_at IS NULL").
		Scan(&result).Error
	if err != nil {
		return nil, err
	}

	// 相册数量
	r.db.Model(&models.Album{}).
		Where("deleted_at IS NULL").
		Select("COUNT(*)").
		Scan(&result.AlbumTotal)

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
func (r *Repository) GetImageTimeStats() (*ImageTimeStats, error) {
	var stats ImageTimeStats
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	// 今日
	r.db.Model(&models.Image{}).
		Where("DATE(created_at) = ? AND deleted_at IS NULL", today).
		Count(&stats.Today)

	// 昨日
	r.db.Model(&models.Image{}).
		Where("DATE(created_at) = ? AND deleted_at IS NULL", yesterday).
		Count(&stats.Yesterday)

	// 本周 (MySQL YEARWEEK)
	r.db.Model(&models.Image{}).
		Where("YEARWEEK(created_at) = YEARWEEK(?) AND deleted_at IS NULL", now).
		Count(&stats.ThisWeek)

	// 本月
	r.db.Model(&models.Image{}).
		Where("YEAR(created_at) = ? AND MONTH(created_at) = ? AND deleted_at IS NULL",
			now.Year(), int(now.Month())).
		Count(&stats.ThisMonth)

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
func (r *Repository) GetStorageStats() ([]StorageStat, error) {
	var stats []StorageStat

	err := r.db.Table("images i").
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
	Date  string
	Count int64
}

// GetDailyStats 获取近 N 天每日统计
func (r *Repository) GetDailyStats(days int) ([]DailyStat, error) {
	var stats []DailyStat

	err := r.db.Table("images").
		Select("DATE(created_at) as date, COUNT(*) as count").
		Where("created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY) AND deleted_at IS NULL", days).
		Group("DATE(created_at)").
		Order("date").
		Scan(&stats).Error

	return stats, err
}
