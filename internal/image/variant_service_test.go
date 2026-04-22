package image

import (
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache"
	configdb "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/utils/format"
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

func TestHandleCompletedVariantsUsesCachedVariants(t *testing.T) {
	provider, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1_000,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = provider.Close()
	})

	cacheHelper := cache.NewHelper(provider, cache.HelperConfig{
		ImageCacheTTL:         time.Hour,
		ImageDataCacheTTL:     time.Hour,
		MaxCacheableImageSize: cache.DefaultMaxCacheableImageSize,
	})
	service := &VariantService{cacheHelper: cacheHelper}
	image := &models.Image{
		ID:            42,
		Identifier:    "img-variant-cache",
		StoragePath:   "original/2026/04/22/img-variant-cache.jpg",
		MimeType:      "image/jpeg",
		VariantStatus: models.ImageVariantStatusCompleted,
	}
	settings := &configdb.ImageProcessingSettings{
		ConversionEnabledFormats: []string{models.FormatWebP, models.FormatAVIF},
	}
	variants := []models.ImageVariant{
		{
			ImageID:     image.ID,
			Identifier:  "img-variant-cache.webp",
			StoragePath: "variants/2026/04/22/img-variant-cache.webp",
			Format:      models.FormatWebP,
			Status:      models.VariantStatusCompleted,
		},
	}

	err = cacheHelper.CacheImageVariants(t.Context(), image.ID, variants)
	require.NoError(t, err)

	result, err := service.handleCompletedVariants(t.Context(), image, "image/avif,image/webp,image/*,*/*", settings)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsOriginal)
	assert.Equal(t, format.FormatWebP, result.Format)
	require.NotNil(t, result.Variant)
	assert.Equal(t, variants[0].Identifier, result.Variant.Identifier)
}
