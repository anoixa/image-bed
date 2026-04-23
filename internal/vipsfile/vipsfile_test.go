package vipsfile

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testStartupOnce sync.Once

func ensureTestStartup(t *testing.T) {
	t.Helper()
	testStartupOnce.Do(func() {
		err := Startup(&vips.Config{
			MaxCacheMem:      1,
			MaxCacheSize:     1,
			MaxCacheFiles:    0,
			ConcurrencyLevel: 1,
		})
		require.NoError(t, err)
	})
}

func TestLoadImageFromFileAndSaveWebP(t *testing.T) {
	ensureTestStartup(t)

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
	ensureTestStartup(t)

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
	assert.Equal(t, "image.png[access=random,fail=TRUE]", buildFileOption("image.png", ImportOptions{Access: "random", FailOnError: true}))
	assert.Equal(t, "image.png", buildFileOption("image.png", ImportOptions{}))
}

func TestLoadImageFromFile_NotFound(t *testing.T) {
	ensureTestStartup(t)

	_, _, err := LoadImageFromFile("/tmp/does-not-exist-image-bed.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load image from file")
}

func TestImageHandleSaveWebPToFile_NilHandle(t *testing.T) {
	ensureTestStartup(t)

	var handle *ImageHandle
	err := handle.SaveWebPToFile(filepath.Join(t.TempDir(), "out.webp"), DefaultWebPOptions())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil vips image")
}

func TestLoadImageFromFileAndSaveAVIF(t *testing.T) {
	ensureTestStartup(t)

	src := writeTestPNG(t, 4, 2, false)
	dst := filepath.Join(t.TempDir(), "out.avif")

	img, info, err := LoadImageFromFile(src)
	require.NoError(t, err)
	defer img.Close()

	assert.Equal(t, 4, info.Width)
	assert.Equal(t, 2, info.Height)

	err = img.SaveAVIFToFile(dst, AVIFOptions{
		Quality:       60,
		Effort:        4,
		StripMetadata: true,
		Bitdepth:      8,
	})
	if err != nil {
		t.Skipf("avif encoder unavailable in current libvips runtime: %v", err)
	}

	stat, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Positive(t, stat.Size())
}

func TestLoadJPEGFromFileAndSaveAVIF(t *testing.T) {
	ensureTestStartup(t)

	src := writeTestJPEG(t, 848, 1200)
	dst := filepath.Join(t.TempDir(), "out.avif")

	img, info, err := LoadImageFromFile(src)
	require.NoError(t, err)
	defer img.Close()

	assert.Equal(t, 848, info.Width)
	assert.Equal(t, 1200, info.Height)

	err = img.SaveAVIFToFile(dst, AVIFOptions{
		Quality:       75,
		Effort:        4,
		StripMetadata: true,
		Bitdepth:      10,
	})
	if err != nil {
		t.Skipf("avif encoder unavailable in current libvips runtime: %v", err)
	}

	stat, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Positive(t, stat.Size())
}

func TestLoadJPEGFromFileSaveWebPThenAVIF(t *testing.T) {
	ensureTestStartup(t)

	src := writeTestJPEG(t, 542, 1200)
	webpDst := filepath.Join(t.TempDir(), "out.webp")
	avifDst := filepath.Join(t.TempDir(), "out.avif")

	img, _, err := LoadImageFromFile(src)
	require.NoError(t, err)
	defer img.Close()

	err = img.SaveWebPToFile(webpDst, WebPOptions{
		Quality:         75,
		ReductionEffort: 4,
		StripMetadata:   true,
	})
	require.NoError(t, err)

	err = img.SaveAVIFToFile(avifDst, AVIFOptions{
		Quality:       75,
		Effort:        4,
		StripMetadata: true,
		Bitdepth:      8,
	})
	if err != nil {
		t.Skipf("avif encoder unavailable in current libvips runtime: %v", err)
	}

	stat, err := os.Stat(avifDst)
	require.NoError(t, err)
	assert.Positive(t, stat.Size())
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

func writeTestJPEG(t *testing.T, width, height int) string {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / max(width-1, 1)),
				G: uint8((y * 255) / max(height-1, 1)),
				B: 120,
				A: 255,
			})
		}
	}

	path := filepath.Join(t.TempDir(), "input.jpg")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.NoError(t, jpeg.Encode(f, img, &jpeg.Options{Quality: 90}))
	return path
}
