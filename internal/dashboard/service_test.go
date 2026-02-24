package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache"
	dashboardRepo "github.com/anoixa/image-bed/database/repo/dashboard"
)

// mockCache 模拟缓存
type mockCache struct {
	data map[string]interface{}
}

func newMockCache() *mockCache {
	return &mockCache{
		data: make(map[string]interface{}),
	}
}

func (m *mockCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *mockCache) Get(ctx context.Context, key string, dest interface{}) error {
	if val, ok := m.data[key]; ok {
		if stats, ok := val.(*StatsResponse); ok {
			*dest.(*StatsResponse) = *stats
			return nil
		}
	}
	return cache.ErrCacheMiss
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockCache) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockCache) Close() error {
	return nil
}

func (m *mockCache) Name() string {
	return "mock"
}

// mockRepository 模拟仓库
type mockRepository struct {
	overviewStats  *dashboardRepo.OverviewStats
	timeStats      *dashboardRepo.ImageTimeStats
	storageStats   []dashboardRepo.StorageStat
	dailyStats     []dashboardRepo.DailyStat
}

func (m *mockRepository) GetOverviewStats() (*dashboardRepo.OverviewStats, error) {
	return m.overviewStats, nil
}

func (m *mockRepository) GetImageTimeStats() (*dashboardRepo.ImageTimeStats, error) {
	return m.timeStats, nil
}

func (m *mockRepository) GetStorageStats() ([]dashboardRepo.StorageStat, error) {
	return m.storageStats, nil
}

func (m *mockRepository) GetDailyStats(days int) ([]dashboardRepo.DailyStat, error) {
	return m.dailyStats, nil
}

func TestService_GetStats(t *testing.T) {
	mockRepo := &mockRepository{
		overviewStats: &dashboardRepo.OverviewStats{
			ImageTotal:   100,
			AlbumTotal:   5,
			UserTotal:    1,
			StorageTotal: 1024 * 1024 * 100, // 100MB
		},
		timeStats: &dashboardRepo.ImageTimeStats{
			Today:     10,
			Yesterday: 5,
			ThisWeek:  20,
			ThisMonth: 50,
		},
		storageStats: []dashboardRepo.StorageStat{
			{StorageID: 1, StorageName: "Local", Count: 60, Size: 60 * 1024 * 1024},
			{StorageID: 2, StorageName: "MinIO", Count: 40, Size: 40 * 1024 * 1024},
		},
		dailyStats: []dashboardRepo.DailyStat{
			{Date: "2024-01-01", Count: 5},
			{Date: "2024-01-02", Count: 3},
		},
	}

	mockCache := newMockCache()
	svc := NewService(mockRepo, mockCache)

	ctx := context.Background()
	stats, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// 验证概览统计
	if stats.Overview.Images.Total != 100 {
		t.Errorf("Expected total images 100, got %d", stats.Overview.Images.Total)
	}
	if stats.Overview.Images.Today != 10 {
		t.Errorf("Expected today images 10, got %d", stats.Overview.Images.Today)
	}
	if stats.Overview.Albums.Total != 5 {
		t.Errorf("Expected total albums 5, got %d", stats.Overview.Albums.Total)
	}

	// 验证存储统计
	if len(stats.StorageStats) != 2 {
		t.Errorf("Expected 2 storage stats, got %d", len(stats.StorageStats))
	}

	// 验证缓存
	cachedStats, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats from cache failed: %v", err)
	}
	if cachedStats.Overview.Images.Total != 100 {
		t.Errorf("Cached stats incorrect")
	}
}

func TestService_RefreshCache(t *testing.T) {
	mockRepo := &mockRepository{
		overviewStats: &dashboardRepo.OverviewStats{
			ImageTotal:   100,
			StorageTotal: 1024 * 1024,
		},
		timeStats:    &dashboardRepo.ImageTimeStats{},
		storageStats: []dashboardRepo.StorageStat{},
		dailyStats:   []dashboardRepo.DailyStat{},
	}

	mockCache := newMockCache()
	svc := NewService(mockRepo, mockCache)

	ctx := context.Background()

	// 先获取一次，写入缓存
	_, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// 验证缓存存在
	exists, _ := mockCache.Exists(ctx, "dashboard:stats")
	if !exists {
		t.Error("Cache should exist")
	}

	// 刷新缓存
	err = svc.RefreshCache(ctx)
	if err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}

	// 验证缓存已被删除
	exists, _ = mockCache.Exists(ctx, "dashboard:stats")
	if exists {
		t.Error("Cache should be deleted after refresh")
	}
}

func Test_buildTrendData(t *testing.T) {
	svc := &Service{}

	// 使用相对于当前时间的日期
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	today := now.Format("2006-01-02")

	dailyStats := []dashboardRepo.DailyStat{
		{Date: yesterday, Count: 5},
		{Date: today, Count: 3},
	}

	trend := svc.buildTrendData(dailyStats, 30)

	if trend.Period != "30d" {
		t.Errorf("Expected period 30d, got %s", trend.Period)
	}
	if len(trend.Dates) != 30 {
		t.Errorf("Expected 30 dates, got %d", len(trend.Dates))
	}
	if len(trend.Data) != 30 {
		t.Errorf("Expected 30 data points, got %d", len(trend.Data))
	}

	// 验证最后两天有数据
	lastIdx := len(trend.Data) - 1
	secondLastIdx := lastIdx - 1

	if trend.Data[secondLastIdx] != 5 {
		t.Errorf("Expected 5 for yesterday, got %d", trend.Data[secondLastIdx])
	}
	if trend.Data[lastIdx] != 3 {
		t.Errorf("Expected 3 for today, got %d", trend.Data[lastIdx])
	}
}
