package image

import (
	"context"
	"io"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

type testStorageProvider struct{}

func (p *testStorageProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *testStorageProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	return nil, nil
}

func (p *testStorageProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *testStorageProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return false, nil
}

func (p *testStorageProvider) Health(ctx context.Context) error {
	return nil
}

func (p *testStorageProvider) Name() string {
	return "test"
}

func TestShouldStartVariantPipeline(t *testing.T) {
	assert.True(t, shouldStartVariantPipeline(true, false, false))
	assert.True(t, shouldStartVariantPipeline(false, true, false))
	assert.True(t, shouldStartVariantPipeline(false, false, true))
	assert.True(t, shouldStartVariantPipeline(true, true, false))
	assert.True(t, shouldStartVariantPipeline(true, false, true))
	assert.True(t, shouldStartVariantPipeline(false, true, true))
	assert.True(t, shouldStartVariantPipeline(true, true, true))
	assert.False(t, shouldStartVariantPipeline(false, false, false))
}

func TestGetStorageForImageDoesNotFallbackForMissingSpecificProvider(t *testing.T) {
	converter := &Converter{storage: &testStorageProvider{}}
	image := &models.Image{StorageConfigID: 999999}

	assert.Nil(t, converter.getStorageForImage(image))
}
