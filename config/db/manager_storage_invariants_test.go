package config

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func bptr(b bool) *bool { return &b }

var storageTestDBCounter atomic.Uint64

// newStorageTestManager uses a per-test unique shared in-memory DB
// (mode=memory&cache=shared with a unique name) so transactional Disable/SetDefault
// see the migrated table while each test stays isolated. cache=private gives each
// pooled connection a separate DB (breaks txns); a fixed shared DSN leaks data
// across tests (breaks crypto init).
func newStorageTestManager(t *testing.T) *Manager {
	t.Helper()
	dsn := fmt.Sprintf("file:storagetest%d?mode=memory&cache=shared", storageTestDBCounter.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemConfig{}))
	manager := NewManager(db, t.TempDir())
	require.NoError(t, manager.crypto.Initialize())
	return manager
}

// createStorage creates a storage config and returns its id.
func createStorage(t *testing.T, mgr *Manager, name string, isDefault, isEnabled bool) uint {
	t.Helper()
	resp, err := mgr.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category:  models.ConfigCategoryStorage,
		Name:      name,
		Config:    map[string]any{"type": "local", "local_path": t.TempDir()},
		IsDefault: bptr(isDefault),
		IsEnabled: bptr(isEnabled),
	}, 0)
	require.NoError(t, err)
	return resp.ID
}

func defaultStorageID(t *testing.T, mgr *Manager) uint {
	t.Helper()
	cfg, err := mgr.repo.GetDefaultByCategory(context.Background(), models.ConfigCategoryStorage)
	require.NoError(t, err)
	return cfg.ID
}

func TestDisableRejectsDefaultStorage(t *testing.T) {
	mgr := newStorageTestManager(t)
	def := createStorage(t, mgr, "def", true, true)
	other := createStorage(t, mgr, "other", false, true)
	assert.Equal(t, def, defaultStorageID(t, mgr))

	err := mgr.Disable(context.Background(), def)
	assert.ErrorIs(t, err, ErrCannotDisableDefaultStorage)
	// default unchanged
	assert.Equal(t, def, defaultStorageID(t, mgr))
	_ = other
}

func TestDisableNonDefaultSucceeds(t *testing.T) {
	mgr := newStorageTestManager(t)
	createStorage(t, mgr, "def", true, true)
	other := createStorage(t, mgr, "other", false, true)

	require.NoError(t, mgr.Disable(context.Background(), other))
	cfg, err := mgr.repo.GetByID(context.Background(), other)
	require.NoError(t, err)
	assert.False(t, cfg.IsEnabled)
}

func TestSetDefaultRejectsDisabledStorage(t *testing.T) {
	mgr := newStorageTestManager(t)
	createStorage(t, mgr, "def", true, true)
	other := createStorage(t, mgr, "other", false, true)

	require.NoError(t, mgr.Disable(context.Background(), other))
	err := mgr.SetDefault(context.Background(), other)
	assert.ErrorIs(t, err, ErrCannotSetDisabledStorageDefault)
}

func TestSetDefaultEnabledSucceeds(t *testing.T) {
	mgr := newStorageTestManager(t)
	createStorage(t, mgr, "def", true, true)
	other := createStorage(t, mgr, "other", false, true)

	require.NoError(t, mgr.SetDefault(context.Background(), other))
	assert.Equal(t, other, defaultStorageID(t, mgr))
}

func TestConcurrentDisableAndSetDefaultPreserveStorageInvariant(t *testing.T) {
	mgr := newStorageTestManager(t)

	for i := 0; i < 20; i++ {
		createStorage(t, mgr, fmt.Sprintf("default-%d", i), true, true)
		target := createStorage(t, mgr, fmt.Sprintf("target-%d", i), false, true)

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = mgr.Disable(context.Background(), target)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = mgr.SetDefault(context.Background(), target)
		}()
		close(start)
		wg.Wait()

		stored, err := mgr.repo.GetByID(context.Background(), target)
		require.NoError(t, err)
		assert.False(t, stored.IsDefault && !stored.IsEnabled,
			"concurrent operations must never leave a disabled default storage")
	}
}

func TestCreateConfigRejectsDisabledDefaultStorage(t *testing.T) {
	mgr := newStorageTestManager(t)
	_, err := mgr.CreateConfig(context.Background(), &models.SystemConfigStoreRequest{
		Category:  models.ConfigCategoryStorage,
		Name:      "bad",
		Config:    map[string]any{"type": "local", "local_path": t.TempDir()},
		IsDefault: bptr(true),
		IsEnabled: bptr(false),
	}, 0)
	assert.ErrorIs(t, err, ErrCannotSetDisabledStorageDefault)
}

func TestCreateConfigPersistsDisabledStorage(t *testing.T) {
	mgr := newStorageTestManager(t)
	id := createStorage(t, mgr, "disabled", false, false)

	stored, err := mgr.repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	assert.False(t, stored.IsEnabled, "explicit false must not be replaced by the GORM default")
}

func TestCreateConfigDefaultClearsOldDefault(t *testing.T) {
	mgr := newStorageTestManager(t)
	first := createStorage(t, mgr, "first", true, true)
	assert.Equal(t, first, defaultStorageID(t, mgr))
	second := createStorage(t, mgr, "second", true, true)
	assert.Equal(t, second, defaultStorageID(t, mgr), "new default must supersede old")
}

func TestUpdateConfigRejectsDisablingDefault(t *testing.T) {
	mgr := newStorageTestManager(t)
	def := createStorage(t, mgr, "def", true, true)

	_, err := mgr.UpdateConfig(context.Background(), def, &models.SystemConfigStoreRequest{
		Category:  models.ConfigCategoryStorage,
		Config:    map[string]any{"type": "local", "local_path": t.TempDir()},
		IsEnabled: bptr(false),
	})
	assert.ErrorIs(t, err, ErrCannotDisableDefaultStorage)
}

func TestUpdateConfigRejectsChangingStorageDefault(t *testing.T) {
	mgr := newStorageTestManager(t)
	createStorage(t, mgr, "def", true, true)
	other := createStorage(t, mgr, "other", false, true)

	_, err := mgr.UpdateConfig(context.Background(), other, &models.SystemConfigStoreRequest{
		Category:  models.ConfigCategoryStorage,
		Config:    map[string]any{"type": "local", "local_path": t.TempDir()},
		IsDefault: bptr(true),
	})
	assert.ErrorIs(t, err, ErrStorageDefaultChangeRequiresEndpoint)
	assert.Equal(t, "def", defaultStorageIDName(t, mgr))
}

func TestUpdateConfigRejectsCategoryMismatch(t *testing.T) {
	mgr := newStorageTestManager(t)
	id := createStorage(t, mgr, "storage", false, true)

	_, err := mgr.UpdateConfig(context.Background(), id, &models.SystemConfigStoreRequest{
		Category: models.ConfigCategoryOAuth,
		Config:   map[string]any{"type": "local", "local_path": t.TempDir()},
	})
	assert.ErrorIs(t, err, ErrConfigCategoryMismatch)
}

func defaultStorageIDName(t *testing.T, mgr *Manager) string {
	t.Helper()
	cfg, err := mgr.repo.GetDefaultByCategory(context.Background(), models.ConfigCategoryStorage)
	require.NoError(t, err)
	return cfg.Name
}
