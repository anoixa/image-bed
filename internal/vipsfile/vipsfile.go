package vipsfile

/*
#cgo pkg-config: vips
#include <stdlib.h>
#include <vips/vips.h>
#include "vipsfile.h"

#if defined(__GLIBC__)
#include <malloc.h>

static int ib_malloc_trim(size_t pad) {
	return malloc_trim(pad);
}
#else
static int ib_malloc_trim(size_t pad) {
	(void)pad;
	return 0;
}
#endif
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/davidbyttow/govips/v2/vips"
)

type ImageInfo struct {
	Width    int
	Height   int
	HasAlpha bool
}

type WebPOptions struct {
	Quality         int
	ReductionEffort int
	StripMetadata   bool
	Lossless        bool
	NearLossless    bool
	IccProfile      string
	MinSize         bool
	MinKeyFrames    int
	MaxKeyFrames    int
}

type AVIFOptions struct {
	Quality       int
	Effort        int
	StripMetadata bool
	Lossless      bool
	Bitdepth      int
}

type ImportOptions struct {
	Access      string
	FailOnError bool
}

type ThumbnailOptions struct {
	Width  int
	Height int
	Crop   int
	Size   int
}

type ImageHandle struct {
	ptr *C.VipsImage
}

var startupOnce sync.Once
var startupErr error
var started bool
var avifSupportOnce sync.Once
var avifSupport bool

var ErrNotInitialized = errors.New("vipsfile not initialized: call vipsfile.Startup before using file-based vips operations")

func Startup(config *vips.Config) error {
	startupOnce.Do(func() {
		startupErr = vips.Startup(config)
		if startupErr == nil {
			started = true
		}
	})
	return startupErr
}

func Shutdown() {
	if started {
		started = false
		vips.Shutdown()
	}
}

func ensureStarted() error {
	if !started {
		return ErrNotInitialized
	}
	return startupErr
}

func SupportsAVIFEncoding() bool {
	if err := ensureStarted(); err != nil {
		return false
	}

	avifSupportOnce.Do(func() {
		avifSupport = C.ib_supports_heifsave() != 0
	})

	return avifSupport
}

func DefaultImportOptions() ImportOptions {
	return ImportOptions{
		Access:      "sequential",
		FailOnError: true,
	}
}

func DefaultWebPOptions() WebPOptions {
	return WebPOptions{
		Quality:         75,
		ReductionEffort: 4,
	}
}

func DefaultAVIFOptions() AVIFOptions {
	return AVIFOptions{
		Quality:       80,
		Effort:        4,
		StripMetadata: true,
		Bitdepth:      8,
	}
}

func DefaultThumbnailOptions(width int) ThumbnailOptions {
	return ThumbnailOptions{
		Width:  width,
		Height: -1,
		Crop:   int(vips.InterestingNone),
		Size:   int(vips.SizeBoth),
	}
}

func buildFileOption(path string, opts ImportOptions) string {
	option := path
	suffix := ""
	hasPrev := false
	if opts.Access != "" {
		suffix += "access=" + opts.Access
		hasPrev = true
	}
	if opts.FailOnError {
		if hasPrev {
			suffix += ","
		}
		suffix += "fail=TRUE"
	}
	if suffix == "" {
		return option
	}
	return option + "[" + suffix + "]"
}

func lastError(op string) error {
	msg := C.GoString(C.vips_error_buffer())
	C.vips_error_clear()
	if msg == "" {
		msg = "libvips operation failed"
	}
	if op == "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("%s: %s", op, msg)
}

func boolToInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func LoadImageFromFile(path string) (*ImageHandle, ImageInfo, error) {
	return LoadImageFromFileWithOptions(path, DefaultImportOptions())
}

func LoadImageFromFileWithOptions(path string, opts ImportOptions) (*ImageHandle, ImageInfo, error) {
	if err := ensureStarted(); err != nil {
		return nil, ImageInfo{}, err
	}

	cPath := C.CString(buildFileOption(path, opts))
	defer C.free(unsafe.Pointer(cPath))

	var img *C.VipsImage
	if C.ib_load_image_from_file(cPath, &img) != 0 {
		return nil, ImageInfo{}, lastError("load image from file")
	}

	info := imageInfoFromVips(img)
	return &ImageHandle{ptr: img}, info, nil
}

func ThumbnailFileToWebP(srcPath, dstPath string, width int, opts WebPOptions) (ImageInfo, error) {
	return ThumbnailFileToWebPWithOptions(srcPath, dstPath, DefaultThumbnailOptions(width), DefaultImportOptions(), opts)
}

func ThumbnailFileToWebPWithOptions(srcPath, dstPath string, thumb ThumbnailOptions, importOpts ImportOptions, webpOpts WebPOptions) (ImageInfo, error) {
	if err := ensureStarted(); err != nil {
		return ImageInfo{}, err
	}

	cSrc := C.CString(buildFileOption(srcPath, importOpts))
	defer C.free(unsafe.Pointer(cSrc))
	cDst := C.CString(dstPath)
	defer C.free(unsafe.Pointer(cDst))
	cProfile := C.CString(webpProfileOrNone(webpOpts.IccProfile))
	defer C.free(unsafe.Pointer(cProfile))

	var img *C.VipsImage
	if C.ib_thumbnail_from_file(cSrc, C.int(thumb.Width), C.int(thumb.Height), C.int(thumb.Crop), C.int(thumb.Size), &img) != 0 {
		return ImageInfo{}, lastError("thumbnail from file")
	}
	defer C.ib_unref_image(img)

	if C.ib_save_webp_file(
		img,
		cDst,
		boolToInt(webpOpts.StripMetadata),
		C.int(webpOpts.Quality),
		boolToInt(webpOpts.Lossless),
		boolToInt(webpOpts.NearLossless),
		C.int(webpOpts.ReductionEffort),
		cProfile,
		boolToInt(webpOpts.MinSize),
		C.int(webpOpts.MinKeyFrames),
		C.int(webpOpts.MaxKeyFrames),
	) != 0 {
		return ImageInfo{}, lastError("save webp to file")
	}

	return imageInfoFromVips(img), nil
}

func (h *ImageHandle) SaveWebPToFile(dstPath string, opts WebPOptions) error {
	if h == nil || h.ptr == nil {
		return fmt.Errorf("nil vips image")
	}

	cDst := C.CString(dstPath)
	defer C.free(unsafe.Pointer(cDst))
	cProfile := C.CString(webpProfileOrNone(opts.IccProfile))
	defer C.free(unsafe.Pointer(cProfile))

	if C.ib_save_webp_file(
		h.ptr,
		cDst,
		boolToInt(opts.StripMetadata),
		C.int(opts.Quality),
		boolToInt(opts.Lossless),
		boolToInt(opts.NearLossless),
		C.int(opts.ReductionEffort),
		cProfile,
		boolToInt(opts.MinSize),
		C.int(opts.MinKeyFrames),
		C.int(opts.MaxKeyFrames),
	) != 0 {
		return lastError("save webp to file")
	}

	return nil
}

func (h *ImageHandle) SaveAVIFToFile(dstPath string, opts AVIFOptions) error {
	if h == nil || h.ptr == nil {
		return fmt.Errorf("nil vips image")
	}

	if opts.Bitdepth == 0 {
		opts.Bitdepth = 8
	}

	cDst := C.CString(dstPath)
	defer C.free(unsafe.Pointer(cDst))

	if C.ib_save_avif_file(
		h.ptr,
		cDst,
		boolToInt(!opts.StripMetadata),
		C.int(opts.Quality),
		boolToInt(opts.Lossless),
		C.int(opts.Effort),
		C.int(opts.Bitdepth),
	) != 0 {
		return lastError("save avif to file")
	}

	return nil
}

func (h *ImageHandle) Close() {
	if h == nil || h.ptr == nil {
		return
	}
	C.ib_unref_image(h.ptr)
	h.ptr = nil
}

func imageInfoFromVips(img *C.VipsImage) ImageInfo {
	var width, height, hasAlpha C.int
	C.ib_get_image_info(img, &width, &height, &hasAlpha)
	return ImageInfo{
		Width:    int(width),
		Height:   int(height),
		HasAlpha: hasAlpha != 0,
	}
}

func webpProfileOrNone(profile string) string {
	if profile == "" {
		return "none"
	}
	return profile
}

// MallocTrim releases free memory from the heap back to the OS.
func MallocTrim() {
	C.ib_malloc_trim(0)
}
