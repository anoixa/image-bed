package images

import (
	"errors"
	"net/http"
	"testing"

	imagesvc "github.com/anoixa/image-bed/internal/image"
	"github.com/anoixa/image-bed/storage"
)

func TestUploadErrorStatusDisabledIs409(t *testing.T) {
	// A disabled-storage error must map to 409, not 503/500.
	disabledErr := errors.Join(storage.ErrProviderDisabled)
	if got := uploadErrorStatus(disabledErr); got != http.StatusConflict {
		t.Fatalf("uploadErrorStatus(disabled) = %d, want 409", got)
	}
	// Unavailable still 503.
	if got := uploadErrorStatus(storage.ErrNoDefaultStorage); got != http.StatusServiceUnavailable {
		t.Fatalf("uploadErrorStatus(unavailable) = %d, want 503", got)
	}
	// Sanity: IsStorageDisabled recognizes the wrapped error.
	if !imagesvc.IsStorageDisabled(disabledErr) {
		t.Fatal("IsStorageDisabled must recognize wrapped ErrProviderDisabled")
	}
}
