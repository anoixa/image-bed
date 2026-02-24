package dashboard

import (
	"context"
	"math"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/dashboard"
	"github.com/anoixa/image-bed/utils/format"
)

// StatsRepository 统计仓库接口
type StatsRepository interface {
	GetOverviewStats() (*dashboard.OverviewStats, error)
	GetImageTimeStats() (*dashboard.ImageTimeStats, error)
	GetStorageStats() ([]dashboard.StorageStat, error)
	GetDailyStats(days int) ([]dashboard.DailyStat, error)
}

// Service Dashboard 统计服务
type Service struct {
	repo     StatsRepository
	cache    cache.Provider
	cacheTTL time.Duration
}

// NewService 创建新的 Dashboard 统计服务
func NewService(repo StatsRepository, cacheProvider cache.Provider) *Service {
	return &Service{
		repo:     repo,
		cache:    cacheProvider,
		cacheTTL: 5 * time.Minute,
	}
}

// StatsResponse Dashboard 统计响应
type StatsResponse struct {
	Overview     OverviewStats     `json:"overview"`
	StorageStats []StorageStatItem `json:"storage_stats"`
	Trend        TrendStats        `json:"trend"`
}

// OverviewStats 概览统计
type OverviewStats struct {
	Images  ImageStats   `json:"images"`
	Albums  CountStats   `json:"albums"`
	Users   CountStats   `json:"users"`
	Storage StorageStats `json:"storage"`
}

// ImageStats 图片统计
type ImageStats struct {
	Total     int64 `json:"total"`
	Today     int64 `json:"today"`
	Yesterday int64 `json:"yesterday"`
	ThisWeek  int64 `json:"this_week"`
	ThisMonth int64 `json:"this_month"`
}

// CountStats 数量统计
type CountStats struct {
	Total int64 `json:"total"`
}

// StorageStats 存储统计
type StorageStats struct {
	TotalSize      int64  `json:"total_size"`
	TotalSizeHuman string `json:"total_size_human"`
}

// StorageStatItem 单个存储统计
type StorageStatItem struct {
	StorageID   uint    `json:"storage_id"`
	StorageName string  `json:"storage_name"`
	Count       int64   `json:"count"`
	Size        int64   `json:"size"`
	SizeHuman   string  `json:"size_human"`
	Percentage  float64 `json:"percentage"`
}

// TrendStats 趋势统计
type TrendStats struct {
	Period string   `json:"period"`
	Dates  []string `json:"dates"`
	Data   []int64  `json:"data"`
}

// GetStats 获取 Dashboard 统计数据
func (s *Service) GetStats(ctx context.Context) (*StatsResponse, error) {
	cacheKey := "dashboard:stats"

	// 尝试从缓存获取
	var cached StatsResponse
	if err := s.cache.Get(ctx, cacheKey, &cached); err == nil {
		return &cached, nil
	}

	// 查询概览统计
	overview, err := s.repo.GetOverviewStats()
	if err != nil {
		return nil, err
	}

	// 查询时间维度统计
	timeStats, err := s.repo.GetImageTimeStats()
	if err != nil {
		return nil, err
	}

	// 查询存储统计
	storageStats, err := s.repo.GetStorageStats()
	if err != nil {
		return nil, err
	}

	// 查询趋势数据
	dailyStats, err := s.repo.GetDailyStats(30)
	if err != nil {
		return nil, err
	}

	// 组装响应
	response := s.buildResponse(overview, timeStats, storageStats, dailyStats)
	
	_ = s.cache.Set(ctx, cacheKey, response, s.cacheTTL)

	return response, nil
}

// RefreshCache 刷新统计数据缓存
func (s *Service) RefreshCache(ctx context.Context) error {
	cacheKey := "dashboard:stats"
	return s.cache.Delete(ctx, cacheKey)
}

// buildResponse 组装响应数据
func (s *Service) buildResponse(
	overview *dashboard.OverviewStats,
	timeStats *dashboard.ImageTimeStats,
	storageStats []dashboard.StorageStat,
	dailyStats []dashboard.DailyStat,
) *StatsResponse {
	// 计算存储总大小用于百分比
	var totalSize int64
	for _, stat := range storageStats {
		totalSize += stat.Size
	}

	// 组装存储统计
	storageItems := make([]StorageStatItem, len(storageStats))
	for i, stat := range storageStats {
		percentage := 0.0
		if totalSize > 0 {
			percentage = float64(stat.Size) / float64(totalSize) * 100
			percentage = math.Round(percentage*100) / 100
		}
		storageItems[i] = StorageStatItem{
			StorageID:   stat.StorageID,
			StorageName: stat.StorageName,
			Count:       stat.Count,
			Size:        stat.Size,
			SizeHuman:   format.HumanReadableSize(stat.Size),
			Percentage:  percentage,
		}
	}

	// 组装趋势数据
	trend := s.buildTrendData(dailyStats, 30)

	return &StatsResponse{
		Overview: OverviewStats{
			Images: ImageStats{
				Total:     overview.ImageTotal,
				Today:     timeStats.Today,
				Yesterday: timeStats.Yesterday,
				ThisWeek:  timeStats.ThisWeek,
				ThisMonth: timeStats.ThisMonth,
			},
			Albums: CountStats{
				Total: overview.AlbumTotal,
			},
			Users: CountStats{
				Total: overview.UserTotal,
			},
			Storage: StorageStats{
				TotalSize:      overview.StorageTotal,
				TotalSizeHuman: format.HumanReadableSize(overview.StorageTotal),
			},
		},
		StorageStats: storageItems,
		Trend:        trend,
	}
}

// buildTrendData 构建趋势数据
func (s *Service) buildTrendData(stats []dashboard.DailyStat, days int) TrendStats {
	dates := make([]string, days)
	data := make([]int64, days)

	now := time.Now()
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		dates[i] = date
	}

	// 构建日期到数量的映射
	statMap := make(map[string]int64)
	for _, stat := range stats {
		statMap[stat.Date] = stat.Count
	}

	// 填充数据，没有数据的天数补0
	for i, date := range dates {
		if count, ok := statMap[date]; ok {
			data[i] = count
		} else {
			data[i] = 0
		}
	}

	return TrendStats{
		Period: "30d",
		Dates:  dates,
		Data:   data,
	}
}
