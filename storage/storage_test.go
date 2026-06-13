package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	registryPtr.Store(&registryState{
		providers: make(map[uint]providerEntry),
	})
}

func TestErrSentinelsAreDistinct(t *testing.T) {
	if !errors.Is(ErrProviderDisabled, ErrProviderDisabled) {
		t.Fatal("ErrProviderDisabled must support errors.Is")
	}
	if errors.Is(ErrProviderDisabled, ErrProviderNotFound) {
		t.Fatal("disabled and not-found must be distinct")
	}
	if errors.Is(ErrProviderDisabled, ErrCannotDisableDefaultProvider) {
		t.Fatal("disabled and cannot-disable-default must be distinct")
	}
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
		IsEnabled: true,
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
		IsEnabled: true,
	}
	cfg2 := StorageConfig{
		ID:        11,
		Name:      "local-test-11",
		Type:      "local",
		LocalPath: filepath.Join(tempDir, "test11"),
		IsDefault: false,
		IsEnabled: true,
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

func TestLocalStoragePathProvider(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	require.NoError(t, err)

	// Write a file to test with
	testPath := "2024/01/test.jpg"
	fullPath := filepath.Join(dir, testPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte("data"), 0600))

	pp, ok := Provider(ls).(PathProvider)
	require.True(t, ok, "LocalStorage must implement PathProvider")

	got, err := pp.GetFilePath(testPath)
	require.NoError(t, err)
	assert.Equal(t, fullPath, got)
}

func TestLocalStoragePathProvider_Traversal(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	require.NoError(t, err)

	pp := Provider(ls).(PathProvider)

	_, err = pp.GetFilePath("../etc/passwd")
	assert.Error(t, err, "path traversal must be rejected")

	_, err = pp.GetFilePath("/etc/passwd")
	assert.Error(t, err, "absolute path must be rejected")
}

// --- writable / disabled semantics ---

func TestGetWritableByIDAndIsWritable(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 1, Name: "on", Type: "local", LocalPath: filepath.Join(tempDir, "on"), IsEnabled: true,
	}))
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 2, Name: "off", Type: "local", LocalPath: filepath.Join(tempDir, "off"), IsEnabled: false,
	}))

	// readable access ignores writable
	if _, err := GetByID(2); err != nil {
		t.Fatalf("GetByID(disabled) must succeed (read): %v", err)
	}
	// writable access enforces it
	if _, err := GetWritableByID(1); err != nil {
		t.Fatalf("GetWritableByID(enabled): %v", err)
	}
	if _, err := GetWritableByID(2); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("GetWritableByID(disabled) = %v, want ErrProviderDisabled", err)
	}
	if _, err := GetWritableByID(999); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetWritableByID(unknown) = %v, want ErrProviderNotFound", err)
	}
	assert.True(t, IsWritable(1))
	assert.False(t, IsWritable(2))
	assert.False(t, IsWritable(999))
	assert.False(t, IsWritable(0), "no default configured -> IsWritable(0) false")
}

func TestResolveWritable(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 7, Name: "def", Type: "local", LocalPath: filepath.Join(tempDir, "def"), IsDefault: true, IsEnabled: true,
	}))
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 8, Name: "off", Type: "local", LocalPath: filepath.Join(tempDir, "off"), IsEnabled: false,
	}))

	p, id, err := ResolveWritable(0)
	require.NoError(t, err)
	assert.Equal(t, uint(7), id)
	assert.NotNil(t, p)
	assert.Equal(t, GetDefaultID(), id, "resolved id must equal GetDefaultID (same snapshot)")

	if _, _, err := ResolveWritable(8); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("ResolveWritable(disabled) = %v, want ErrProviderDisabled", err)
	}
	if _, _, err := ResolveWritable(999); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("ResolveWritable(unknown) = %v, want ErrProviderNotFound", err)
	}
}

func TestAddOrUpdateProviderDefaultPointerRefreshAndClear(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)

	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 1, Name: "v1", Type: "local", LocalPath: filepath.Join(tempDir, "1"), IsDefault: true, IsEnabled: true,
	}))
	assert.Equal(t, uint(1), GetDefaultID())

	// update same default id (still IsDefault&&IsEnabled) -> default must remain set to id 1
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 1, Name: "v2", Type: "local", LocalPath: filepath.Join(tempDir, "1b"), IsDefault: true, IsEnabled: true,
	}))
	assert.Equal(t, uint(1), GetDefaultID(), "default must remain on id 1 after in-place update")
	assert.NotNil(t, GetDefault(), "default provider must remain set after in-place update")

	// flip current default to non-default -> dangling default cleared
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 1, Name: "v2", Type: "local", LocalPath: filepath.Join(tempDir, "1b"), IsDefault: false, IsEnabled: true,
	}))
	assert.Equal(t, uint(0), GetDefaultID(), "clearing is_default on current default must null default")
	assert.Nil(t, GetDefault())

	// disable the current default -> dangling default cleared
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 2, Name: "d2", Type: "local", LocalPath: filepath.Join(tempDir, "2"), IsDefault: true, IsEnabled: true,
	}))
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 2, Name: "d2", Type: "local", LocalPath: filepath.Join(tempDir, "2"), IsDefault: true, IsEnabled: false,
	}))
	assert.Equal(t, uint(0), GetDefaultID(), "disabling current default must null default")
}

func TestSetDefaultIDRejectsDisabled(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 5, Name: "off", Type: "local", LocalPath: filepath.Join(tempDir, "5"), IsEnabled: false,
	}))
	if err := SetDefaultID(5); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("SetDefaultID(disabled) = %v, want ErrProviderDisabled", err)
	}
}

func TestSetProviderWritableGuardsDefault(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 9, Name: "def", Type: "local", LocalPath: filepath.Join(tempDir, "9"), IsDefault: true, IsEnabled: true,
	}))
	if err := SetProviderWritable(9, false); !errors.Is(err, ErrCannotDisableDefaultProvider) {
		t.Fatalf("SetProviderWritable(default,false) = %v, want ErrCannotDisableDefaultProvider", err)
	}
	// non-default toggle
	require.NoError(t, AddOrUpdateProvider(StorageConfig{
		ID: 10, Name: "aux", Type: "local", LocalPath: filepath.Join(tempDir, "10"), IsEnabled: true,
	}))
	require.NoError(t, SetProviderWritable(10, false))
	if _, err := GetWritableByID(10); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("after disable, GetWritableByID = %v, want ErrProviderDisabled", err)
	}
	// re-enable
	require.NoError(t, SetProviderWritable(10, true))
	if _, err := GetWritableByID(10); err != nil {
		t.Fatalf("after re-enable, GetWritableByID = %v", err)
	}
	// unknown id is no-op
	if err := SetProviderWritable(999, false); err != nil {
		t.Fatalf("SetProviderWritable(unknown) = %v, want nil", err)
	}
}

func TestInitStoragePublishesReadableBeforeError(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)
	err := InitStorage([]StorageConfig{
		{ID: 1, Name: "off", Type: "local", LocalPath: filepath.Join(tempDir, "off"), IsEnabled: false},
	})
	require.Error(t, err, "no writable default -> error")
	// disabled provider must still be readable
	p, gerr := GetByID(1)
	require.NoError(t, gerr)
	assert.NotNil(t, p)
	if _, werr := GetWritableByID(1); !errors.Is(werr, ErrProviderDisabled) {
		t.Fatalf("disabled provider must not be writable: %v", werr)
	}
}

func TestInitStorageDisabledDefaultDegradesToEnabled(t *testing.T) {
	tempDir := setupTestDir(t)
	resetStorage(t)
	err := InitStorage([]StorageConfig{
		{ID: 1, Name: "off-but-default", Type: "local", LocalPath: filepath.Join(tempDir, "1"), IsDefault: true, IsEnabled: false},
		{ID: 2, Name: "on", Type: "local", LocalPath: filepath.Join(tempDir, "2"), IsEnabled: true},
	})
	require.NoError(t, err, "degraded success expected")
	assert.Equal(t, uint(2), GetDefaultID(), "default should fall back to enabled provider")
}
