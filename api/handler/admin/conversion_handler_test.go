package admin

import (
	"testing"

	config "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConversionConfigUpdate(t *testing.T) {
	t.Run("allows avif when runtime supports it", func(t *testing.T) {
		current := &config.ImageProcessingSettings{
			ConversionEnabledFormats: []string{models.FormatWebP, models.FormatAVIF},
		}
		req := &UpdateConfigRequest{}

		err := validateConversionConfigUpdate(req, current, true)
		require.NoError(t, err)
	})

	t.Run("rejects enabling avif when runtime does not support it", func(t *testing.T) {
		current := &config.ImageProcessingSettings{
			ConversionEnabledFormats: []string{models.FormatWebP},
		}
		req := &UpdateConfigRequest{
			ConversionEnabledFormats: []string{models.FormatWebP, models.FormatAVIF},
		}

		err := validateConversionConfigUpdate(req, current, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "avif conversion is not supported")
	})

	t.Run("allows explicit disable of avif when runtime does not support it", func(t *testing.T) {
		current := &config.ImageProcessingSettings{
			ConversionEnabledFormats: []string{models.FormatWebP},
		}
		req := &UpdateConfigRequest{
			ConversionEnabledFormats: []string{models.FormatWebP},
		}

		err := validateConversionConfigUpdate(req, current, false)
		require.NoError(t, err)
	})

	t.Run("rejects unrelated update while unsupported avif remains enabled", func(t *testing.T) {
		current := &config.ImageProcessingSettings{
			ConversionEnabledFormats: []string{models.FormatWebP, models.FormatAVIF},
		}
		req := &UpdateConfigRequest{
			WebPQuality: intPtr(80),
		}

		err := validateConversionConfigUpdate(req, current, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "avif conversion is not supported")
	})
}

func intPtr(v int) *int {
	return &v
}
