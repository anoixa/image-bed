package image

import (
	"errors"
	"testing"

	"github.com/anoixa/image-bed/storage"
)

func TestIsStorageUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "generic", err: errors.New("boom"), want: false},
		{name: "no_default", err: storage.ErrNoDefaultStorage, want: true},
		{name: "provider_not_found", err: storage.ErrProviderNotFound, want: true},
		{name: "wrapped_not_found", err: errorsWrap(storage.ErrProviderNotFound), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStorageUnavailable(tc.err); got != tc.want {
				t.Fatalf("IsStorageUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// errorsWrap 用 %w 包装，模拟下层封装后的错误链。
func errorsWrap(err error) error {
	return errors.Join(errors.New("get storage provider"), err)
}

func TestCheckStorageAvailable(t *testing.T) {
	// 未加载的 ID 应被判定为不可用。
	if err := CheckStorageAvailable(99999); !IsStorageUnavailable(err) {
		t.Fatalf("expected unavailable for missing provider, got %v", err)
	}

	// 配置好默认本地存储后，默认与该 ID 都应可用。
	const id = 7
	if err := storage.InitStorage([]storage.StorageConfig{{
		ID:        id,
		Name:      "local-test",
		Type:      "local",
		IsDefault: true,
		IsEnabled: true,
		LocalPath: t.TempDir(),
	}}); err != nil {
		t.Fatalf("InitStorage: %v", err)
	}

	if err := CheckStorageAvailable(0); err != nil {
		t.Fatalf("expected default storage available, got %v", err)
	}
	if err := CheckStorageAvailable(id); err != nil {
		t.Fatalf("expected provider %d available, got %v", id, err)
	}
}
