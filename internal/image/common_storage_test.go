package image

import (
	"errors"
	"testing"

	"github.com/anoixa/image-bed/storage"
)

func TestReadableVsWritableStorageHelpers(t *testing.T) {
	storage.ResetForTest()
	t.Cleanup(storage.ResetForTest)

	// 一个可写 + 一个禁用的 provider。
	if err := storage.AddOrUpdateProvider(storage.StorageConfig{
		ID: 1, Name: "on", Type: "local", LocalPath: t.TempDir(), IsEnabled: true,
	}); err != nil {
		t.Fatalf("add enabled: %v", err)
	}
	if err := storage.AddOrUpdateProvider(storage.StorageConfig{
		ID: 2, Name: "off", Type: "local", LocalPath: t.TempDir(), IsEnabled: false,
	}); err != nil {
		t.Fatalf("add disabled: %v", err)
	}

	// 读访问（删除/serve/去重检查）对禁用存储仍可用。
	if _, err := getReadableStorageProviderByID(2); err != nil {
		t.Fatalf("readable(disabled) must succeed: %v", err)
	}

	// 写访问（上传）对禁用存储返回 disabled。
	_, err := getWritableStorageProviderByID(2)
	if !errors.Is(err, storage.ErrProviderDisabled) {
		t.Fatalf("writable(disabled) = %v, want ErrProviderDisabled", err)
	}
	if !IsStorageDisabled(err) {
		t.Fatal("IsStorageDisabled must recognize ErrProviderDisabled")
	}

	// CheckStorageAvailable 走写路径，禁用存储不可用。
	if cerr := CheckStorageAvailable(2); !IsStorageDisabled(cerr) {
		t.Fatalf("CheckStorageAvailable(disabled) = %v, want disabled", cerr)
	}
}

func TestResolveWritableStorageForUpload(t *testing.T) {
	storage.ResetForTest()
	t.Cleanup(storage.ResetForTest)

	if err := storage.AddOrUpdateProvider(storage.StorageConfig{
		ID: 42, Name: "def", Type: "local", LocalPath: t.TempDir(), IsDefault: true, IsEnabled: true,
	}); err != nil {
		t.Fatalf("add default: %v", err)
	}
	if err := storage.AddOrUpdateProvider(storage.StorageConfig{
		ID: 7, Name: "off", Type: "local", LocalPath: t.TempDir(), IsEnabled: false,
	}); err != nil {
		t.Fatalf("add disabled: %v", err)
	}

	// id==0 解析默认，返回 registry 默认 ID（与写入目标一致）。
	provider, id, err := resolveWritableStorageForUpload(0)
	if err != nil || id != 42 || provider == nil {
		t.Fatalf("resolve(0) = (id=%d, err=%v), want id 42", id, err)
	}

	// 禁用存储上传被拒。
	if _, _, err := resolveWritableStorageForUpload(7); !IsStorageDisabled(err) {
		t.Fatalf("resolve(disabled) = %v, want disabled", err)
	}
}
