package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldStartVariantPipeline(t *testing.T) {
	assert.True(t, shouldStartVariantPipeline(true, false))
	assert.True(t, shouldStartVariantPipeline(false, true))
	assert.True(t, shouldStartVariantPipeline(true, true))
	assert.False(t, shouldStartVariantPipeline(false, false))
}
