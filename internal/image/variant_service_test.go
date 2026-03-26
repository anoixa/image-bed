package image

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleOriginalWithConversionSetsTriggerHint(t *testing.T) {
	service := &VariantService{}
	img := &models.Image{
		Identifier:  "img-1",
		StoragePath: "original/2026/03/25/img-1.jpg",
		MimeType:    "image/jpeg",
	}

	result, err := service.handleOriginalWithConversion(img, true)
	require.NoError(t, err)
	assert.True(t, result.IsOriginal)
	assert.True(t, result.ShouldTriggerConversion)

	result, err = service.handleOriginalWithConversion(img, false)
	require.NoError(t, err)
	assert.True(t, result.IsOriginal)
	assert.False(t, result.ShouldTriggerConversion)
}

func TestHandleOriginalWithConversionKeepsOriginalMetadata(t *testing.T) {
	service := &VariantService{}
	img := &models.Image{
		Identifier:  "img-2",
		StoragePath: "original/2026/03/25/img-2.jpg",
		MimeType:    "image/jpeg",
	}

	result, err := service.handleOriginalWithConversion(img, true)
	require.NoError(t, err)
	assert.Equal(t, img.Identifier, result.Identifier)
	assert.Equal(t, img.StoragePath, result.StoragePath)
	assert.Equal(t, img.MimeType, result.MIMEType)
}
