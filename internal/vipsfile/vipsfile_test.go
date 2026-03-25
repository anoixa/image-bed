package vipsfile

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadImageFromFileAndSaveWebP(t *testing.T) {
	src := writeTestPNG(t, 4, 2, true)
	dst := filepath.Join(t.TempDir(), "out.webp")

	img, info, err := LoadImageFromFile(src)
	require.NoError(t, err)
	defer img.Close()

	assert.Equal(t, 4, info.Width)
	assert.Equal(t, 2, info.Height)
	assert.True(t, info.HasAlpha)

	err = img.SaveWebPToFile(dst, WebPOptions{
		Quality:         80,
		ReductionEffort: 4,
		StripMetadata:   true,
	})
	require.NoError(t, err)

	stat, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Positive(t, stat.Size())
}

func TestThumbnailFileToWebP(t *testing.T) {
	src := writeTestPNG(t, 4, 2, false)
	dst := filepath.Join(t.TempDir(), "thumb.webp")

	info, err := ThumbnailFileToWebP(src, dst, 2, WebPOptions{
		Quality:         80,
		ReductionEffort: 4,
		StripMetadata:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, 2, info.Width)
	assert.Equal(t, 1, info.Height)
	assert.False(t, info.HasAlpha)

	stat, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Positive(t, stat.Size())
}

func TestBuildFileOption(t *testing.T) {
	assert.Equal(t, "image.png[access=sequential,fail=TRUE]", buildFileOption("image.png", DefaultImportOptions()))
	assert.Equal(t, "image.png", buildFileOption("image.png", ImportOptions{}))
}

func TestLoadImageFromFile_NotFound(t *testing.T) {
	_, _, err := LoadImageFromFile("/tmp/does-not-exist-image-bed.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load image from file")
}

func TestImageHandleSaveWebPToFile_NilHandle(t *testing.T) {
	var handle *ImageHandle
	err := handle.SaveWebPToFile(filepath.Join(t.TempDir(), "out.webp"), DefaultWebPOptions())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil vips image")
}

func writeTestPNG(t *testing.T, width, height int, alpha bool) string {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fill := color.NRGBA{R: 30, G: 120, B: 200, A: 255}
	if alpha {
		fill.A = 180
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}

	path := filepath.Join(t.TempDir(), "input.png")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.NoError(t, png.Encode(f, img))
	return path
}
