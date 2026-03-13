package images

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

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
