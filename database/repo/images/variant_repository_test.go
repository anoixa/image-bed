package images

import (
	"fmt"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

func TestCalculateBackoff(t *testing.T) {
	base := 5 * time.Minute

	tests := []struct {
		retryCount int
		want       time.Duration
	}{
		{0, 5 * time.Minute},
		{1, 10 * time.Minute},
		{2, 20 * time.Minute},
		{3, 40 * time.Minute},
		{4, 80 * time.Minute},
		{5, 60 * time.Minute}, // max capped at 60min
		{10, 60 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("retry_%d", tt.retryCount), func(t *testing.T) {
			got := calculateBackoff(base, tt.retryCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVariantStatusConstants(t *testing.T) {
	assert.Equal(t, "pending", models.VariantStatusPending)
	assert.Equal(t, "processing", models.VariantStatusProcessing)
	assert.Equal(t, "completed", models.VariantStatusCompleted)
	assert.Equal(t, "failed", models.VariantStatusFailed)
}

func TestFormatConstants(t *testing.T) {
	assert.Equal(t, "webp", models.FormatWebP)
	assert.Equal(t, "avif", models.FormatAVIF)
}
