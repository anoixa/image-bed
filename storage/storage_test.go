package storage

import (
	"path/filepath"
	"sync"
	"testing"
)

// setupTestDir 创建测试用的临时目录
func setupTestDir(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	return tempDir
}

// resetStorage 重置存储状态用于测试隔离
func resetStorage(t *testing.T) {
	t.Helper()
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = make(map[uint]Provider)
	defaultProvider = nil
	defaultID = 0
}

// TestConcurrentAccess 测试并发访问 providers map
func TestConcurrentAccess(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)
	_ = InitStorage([]StorageConfig{})

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 50

	// 并发读取
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = GetDefault()
				_ = GetDefaultID()
				_, _ = GetByID(0)
				_ = ListProviderIDs()
				_ = GetProviderCount()
			}
		}()
	}

	// 并发添加/更新存储
	wg.Add(numGoroutines / 2)
	for i := 0; i < numGoroutines/2; i++ {
		go func(id uint) {
			defer wg.Done()
			cfg := StorageConfig{
				ID:        id,
				Name:      "test-local",
				Type:      "local",
				LocalPath: filepath.Join(tempDir, "test"),
				IsDefault: false,
			}
			for j := 0; j < numOperations/2; j++ {
				_ = AddOrUpdateProvider(cfg)
			}
		}(uint(i) + 100)
	}

	// 并发切换默认存储
	wg.Add(numGoroutines / 4)
	for i := 0; i < numGoroutines/4; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations/5; j++ {
				_ = SetDefaultID(0)
			}
		}()
	}

	wg.Wait()
}

// TestAddOrUpdateProvider 测试添加/更新存储提供者
func TestAddOrUpdateProvider(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	// 添加本地存储
	cfg := StorageConfig{
		ID:        1,
		Name:      "local-test",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test1"),
		IsDefault: false,
	}

	err := AddOrUpdateProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to add provider: %v", err)
	}

	provider, err := GetByID(1)
	if err != nil {
		t.Fatalf("Failed to get provider: %v", err)
	}
	if provider == nil {
		t.Fatal("Provider should not be nil")
	}

	cfg.IsDefault = true
	err = AddOrUpdateProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to update provider: %v", err)
	}

	if GetDefaultID() != 1 {
		t.Fatalf("Default ID should be 1, got %d", GetDefaultID())
	}
}

// TestRemoveProvider 测试移除存储提供者
func TestRemoveProvider(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	// 添加测试存储
	cfg := StorageConfig{
		ID:        2,
		Name:      "local-test-2",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test2"),
		IsDefault: false,
	}

	err := AddOrUpdateProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to add provider: %v", err)
	}

	_, err = GetByID(2)
	if err != nil {
		t.Fatalf("Provider should exist: %v", err)
	}

	// 移除存储
	err = RemoveProvider(2)
	if err != nil {
		t.Fatalf("Failed to remove provider: %v", err)
	}

	_, err = GetByID(2)
	if err == nil {
		t.Fatal("Provider should not exist after removal")
	}
}

// TestRemoveDefaultProvider 测试移除默认存储（应该失败）
func TestRemoveDefaultProvider(t *testing.T) {
	resetStorage(t)
	_ = InitStorage([]StorageConfig{})

	defaultID := GetDefaultID()

	err := RemoveProvider(defaultID)
	if err == nil {
		t.Fatal("Should not be able to remove default provider")
	}
}

// TestSetDefaultID 测试切换默认存储
func TestSetDefaultID(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	// 添加两个存储
	cfg1 := StorageConfig{
		ID:        10,
		Name:      "local-test-10",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test10"),
		IsDefault: true,
	}
	cfg2 := StorageConfig{
		ID:        11,
		Name:      "local-test-11",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test11"),
		IsDefault: false,
	}

	_ = AddOrUpdateProvider(cfg1)
	_ = AddOrUpdateProvider(cfg2)

	if GetDefaultID() != 10 {
		t.Fatalf("Default ID should be 10, got %d", GetDefaultID())
	}

	// 切换默认存储
	err := SetDefaultID(11)
	if err != nil {
		t.Fatalf("Failed to set default: %v", err)
	}

	if GetDefaultID() != 11 {
		t.Fatalf("Default ID should be 11, got %d", GetDefaultID())
	}

	// 测试切换到不存在的存储
	err = SetDefaultID(999)
	if err == nil {
		t.Fatal("Should not be able to set non-existent provider as default")
	}
}

// TestListProviderIDs 测试列出所有存储ID
func TestListProviderIDs(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	// 添加多个存储
	for i := uint(20); i < 25; i++ {
		cfg := StorageConfig{
			ID:        i,
			Name:      "local-test",
			Type:      "local",
			LocalPath: filepath.Join(tempDir, "test"),
		}
		_ = AddOrUpdateProvider(cfg)
	}

	ids := ListProviderIDs()
	if len(ids) != 5 {
		t.Fatalf("Should have 5 providers, got %d", len(ids))
	}
}

// TestGetProviderCount 测试获取存储数量
func TestGetProviderCount(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	if GetProviderCount() != 0 {
		t.Fatalf("Should have 0 providers initially, got %d", GetProviderCount())
	}

	cfg := StorageConfig{
		ID:        30,
		Name:      "local-test",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test"),
	}
	_ = AddOrUpdateProvider(cfg)

	if GetProviderCount() != 1 {
		t.Fatalf("Should have 1 provider, got %d", GetProviderCount())
	}
}
