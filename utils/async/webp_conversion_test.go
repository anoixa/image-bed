package async

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType ErrorType
	}{
		{
			name:     "Unsupported format - permanent",
			err:      errors.New("unsupported format: image/bmp"),
			wantType: ErrorPermanent,
		},
		{
			name:     "Image corrupt - permanent",
			err:      errors.New("image corrupt: bad data"),
			wantType: ErrorPermanent,
		},
		{
			name:     "Invalid image - permanent",
			err:      errors.New("cannot decode image"),
			wantType: ErrorPermanent,
		},
		{
			name:     "Invalid quality - config",
			err:      errors.New("invalid quality: 200"),
			wantType: ErrorConfig,
		},
		{
			name:     "Quality out of range - config",
			err:      errors.New("quality out of range"),
			wantType: ErrorConfig,
		},
		{
			name:     "Network timeout - transient",
			err:      errors.New("timeout connecting to storage"),
			wantType: ErrorTransient,
		},
		{
			name:     "Generic error - transient",
			err:      errors.New("something went wrong"),
			wantType: ErrorTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err)
			if got != tt.wantType {
				t.Errorf("ClassifyError() = %v, want %v", got, tt.wantType)
			}
		})
	}
}
