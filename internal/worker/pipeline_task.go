package worker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/vipsfile"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/generator"
	"github.com/anoixa/image-bed/utils/pool"
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"runtime"
)

var pipelineLog = utils.ForModule("Pipeline")

// VariantRepository 变体仓库接口
type VariantRepository interface {
	UpdateStatusCAS(id uint, expected, newStatus, errMsg string) (bool, error)
	UpdateCompleted(id uint, identifier, storagePath string, fileSize int64, fileHash string, width, height int) error
	UpdateFailed(id uint, errMsg string) error
	TouchProcessing(ids []uint) error
	GetByID(id uint) (*models.ImageVariant, error)
	DeleteVariant(id uint) error
	ResetStaleProcessing(olderThan time.Duration) (int64, error)
}

// ImageRepository 图片仓库接口
type ImageRepository interface {
	UpdateVariantStatus(imageID uint, status models.ImageVariantStatus) error
	TouchVariantProcessingStatus(imageID uint) error
	GetImageByID(id uint) (*models.Image, error)
}

// pipelineResult 处理结果
type pipelineResult struct {
	StoragePath string
	Width       int
	Height      int
	FileSize    int64
	FileHash    string
}

// readImageDimensions reads image width and height from the file header without full decode.
// Returns (width, height, ok). If the format is unrecognized, ok is false.
func readImageDimensions(filePath string) (width, height int, ok bool) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

func createVariantTempPath() (string, func(), error) {
	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(config.TempDir, "variant-output-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("close temp file: %w", err)
	}

	return path, func() { _ = os.Remove(path) }, nil
}

func stageVariantFileFromPath(path string) (*os.File, int64, string, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, "", nil, fmt.Errorf("open temp file: %w", err)
	}

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}

	stat, err := file.Stat()
	if err != nil {
		cleanup()
		return nil, 0, "", nil, fmt.Errorf("stat temp file: %w", err)
	}

	hasher := sha256.New()
	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)
	if _, err := io.CopyBuffer(hasher, file, *bufPtr); err != nil {
		cleanup()
		return nil, 0, "", nil, fmt.Errorf("hash temp file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, "", nil, fmt.Errorf("reset temp file: %w", err)
	}

	return file, stat.Size(), fmt.Sprintf("%x", hasher.Sum(nil)), cleanup, nil
}

// ImageComplexity 图片复杂度级别
type ImageComplexity int

const (
	ComplexityLow    ImageComplexity = 0 // 低复杂度（截图、纯色）
	ComplexityMedium ImageComplexity = 1 // 中复杂度（普通图）
	ComplexityHigh   ImageComplexity = 2 // 高复杂度（照片）
)

// detectImageComplexity detects image complexity level.
// fileSize is the compressed file size in bytes (used to estimate compression ratio).
func detectImageComplexity(info vipsfile.ImageInfo, fileSize int64) ImageComplexity {
	if info.HasAlpha {
		return ComplexityLow
	}

	pixelCount := info.Width * info.Height

	if pixelCount == 0 || fileSize == 0 {
		return ComplexityMedium
	}

	bytesPerPixel := float64(fileSize) / float64(pixelCount)

	switch {
	case bytesPerPixel > 3.0:
		return ComplexityLow
	case bytesPerPixel > 2.0:
		return ComplexityLow
	case bytesPerPixel < 0.8:
		return ComplexityHigh
	case bytesPerPixel < 1.2:
		return ComplexityMedium
	default:
		return ComplexityMedium
	}
}

// adaptiveWebPQuality 根据图片复杂度返回自适应质量
func adaptiveWebPQuality(complexity ImageComplexity, baseQuality int) int {
	switch complexity {
	case ComplexityLow:
		return min(baseQuality-10, 75)
	case ComplexityMedium:
		return baseQuality
	case ComplexityHigh:
		return min(baseQuality+5, 90)
	default:
		return baseQuality
	}
}

// ImagePipelineTask 统一图片处理任务
type ImagePipelineTask struct {
	ThumbVariantID  uint
	WebPVariantID   uint
	AVIFVariantID   uint
	ImageID         uint
	StoragePath     string
	ImageIdentifier string
	FileSize        int64  // used by detectImageComplexity instead of len(fileBytes)
	MimeType        string // used for GIF guard in generateThumbnail
	Storage         storage.Provider
	Settings        *dbconfig.ImageProcessingSettings
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	CacheHelper     *cache.Helper
}

const avifMinSavingsPercent int64 = 5

var processingHeartbeatInterval = 4 * time.Minute

// getProcessingFilePath returns an OS file path suitable for vips file-based APIs.
// For local storage it returns the stored file path directly (no I/O).
// For remote storage it downloads to a temp file bounded by maxSize.
// Caller must invoke the returned cleanup func exactly once via defer.
func (t *ImagePipelineTask) getProcessingFilePath(ctx context.Context) (path string, cleanup func(), err error) {
	noop := func() {}

	// Local storage: return path directly, no temp file needed
	if pp, ok := t.Storage.(storage.PathProvider); ok {
		p, e := pp.GetFilePath(t.StoragePath)
		if e != nil {
			return "", noop, fmt.Errorf("get file path: %w", e)
		}
		return p, noop, nil
	}

	// Remote storage: download to temp file
	maxSize := int64(50) * 1024 * 1024
	if t.Settings != nil && t.Settings.MaxFileSizeMB > 0 {
		maxSize = int64(t.Settings.MaxFileSizeMB) * 1024 * 1024
	}

	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return "", noop, fmt.Errorf("create temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(config.TempDir, "pipeline-proc-*")
	if err != nil {
		return "", noop, fmt.Errorf("create temp file: %w", err)
	}

	cleanupFn := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}

	stream, err := t.Storage.GetWithContext(ctx, t.StoragePath)
	if err != nil {
		cleanupFn()
		return "", noop, fmt.Errorf("get stream: %w", err)
	}
	if closer, ok := stream.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	bufPtr := pool.SharedBufferPool.Get().(*[]byte)
	defer pool.SharedBufferPool.Put(bufPtr)

	lr := io.LimitReader(stream, maxSize+1)
	n, copyErr := io.CopyBuffer(tmp, lr, *bufPtr)
	if copyErr != nil {
		cleanupFn()
		return "", noop, fmt.Errorf("write temp file: %w", copyErr)
	}
	if n > maxSize {
		cleanupFn()
		return "", noop, fmt.Errorf("file exceeds max size %d bytes", maxSize)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) // avoid double-close; file may already be partially closed
		return "", noop, fmt.Errorf("close temp file: %w", err)
	}

	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

// Execute 执行任务
func (t *ImagePipelineTask) Execute() {
	var acquiredVariants []uint
	defer t.finalize(&acquiredVariants)
	defer t.cleanupAfterPipeline()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if t.ThumbVariantID > 0 {
		pipelineLog.Debugf("Attempting CAS: thumbnail variant %d pending->processing", t.ThumbVariantID)
		acquired, err := t.VariantRepo.UpdateStatusCAS(
			t.ThumbVariantID,
			models.VariantStatusPending,
			models.VariantStatusProcessing,
			"",
		)
		if err != nil {
			pipelineLog.Debugf("CAS error for thumbnail variant %d: %v", t.ThumbVariantID, err)
			return
		}
		if !acquired {
			pipelineLog.Debugf("CAS failed for thumbnail variant %d (not in pending state)", t.ThumbVariantID)
			return
		}
		acquiredVariants = append(acquiredVariants, t.ThumbVariantID)
		pipelineLog.Debugf("CAS success: thumbnail variant %d is now processing", t.ThumbVariantID)
	}

	if t.WebPVariantID > 0 {
		pipelineLog.Debugf("Attempting CAS: WebP variant %d pending->processing", t.WebPVariantID)
		acquired, err := t.VariantRepo.UpdateStatusCAS(
			t.WebPVariantID,
			models.VariantStatusPending,
			models.VariantStatusProcessing,
			"",
		)
		if err != nil {
			pipelineLog.Debugf("CAS error for WebP variant %d: %v", t.WebPVariantID, err)
			return
		}
		if !acquired {
			pipelineLog.Debugf("CAS failed for WebP variant %d (not in pending state)", t.WebPVariantID)
			return
		}
		acquiredVariants = append(acquiredVariants, t.WebPVariantID)
		pipelineLog.Debugf("CAS success: WebP variant %d is now processing", t.WebPVariantID)
	}

	if t.AVIFVariantID > 0 {
		pipelineLog.Debugf("Attempting CAS: AVIF variant %d pending->processing", t.AVIFVariantID)
		acquired, err := t.VariantRepo.UpdateStatusCAS(
			t.AVIFVariantID,
			models.VariantStatusPending,
			models.VariantStatusProcessing,
			"",
		)
		if err != nil {
			pipelineLog.Debugf("CAS error for AVIF variant %d: %v", t.AVIFVariantID, err)
			return
		}
		if !acquired {
			pipelineLog.Debugf("CAS failed for AVIF variant %d (not in pending state)", t.AVIFVariantID)
			return
		}
		acquiredVariants = append(acquiredVariants, t.AVIFVariantID)
		pipelineLog.Debugf("CAS success: AVIF variant %d is now processing", t.AVIFVariantID)
	}

	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		pipelineLog.Debugf("Failed to acquire semaphore: %v", err)
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("semaphore: %v", err))
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("semaphore: %v", err))
		}
		if t.AVIFVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.AVIFVariantID, fmt.Sprintf("semaphore: %v", err))
		}
		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
		t.deleteCacheOnTerminalState("failed")
		return
	}
	defer semaphore.Release()
	stopHeartbeat := t.startProcessingHeartbeat(ctx, acquiredVariants)
	defer stopHeartbeat()

	pipelineLog.Debugf("Starting processing for image=%s, thumbVariant=%d, webpVariant=%d, avifVariant=%d",
		t.StoragePath, t.ThumbVariantID, t.WebPVariantID, t.AVIFVariantID)
	if err := t.runPipeline(ctx); err != nil {
		pipelineLog.Debugf("Processing failed: %v", err)
		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
		t.deleteCacheOnTerminalState("failed")
		return
	}

	pipelineLog.Debugf("Processing success for image=%s", t.ImageIdentifier)
}

// runPipeline 执行处理流水线
func (t *ImagePipelineTask) runPipeline(ctx context.Context) error {
	filePath, cleanup, err := t.getProcessingFilePath(ctx)
	if err != nil {
		if t.ThumbVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, fmt.Sprintf("get file: %v", err))
		}
		if t.WebPVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("get file: %v", err))
		}
		if t.AVIFVariantID > 0 {
			_ = t.VariantRepo.UpdateFailed(t.AVIFVariantID, fmt.Sprintf("get file: %v", err))
		}
		return fmt.Errorf("get processing file: %w", err)
	}
	defer cleanup()

	var thumbResult, webpResult, avifResult *pipelineResult
	var hasSuccess, hasFailed bool
	var thumbSkipped, webpSkipped, avifSkipped bool

	if t.ThumbVariantID > 0 {
		result, err := t.generateThumbnail(ctx, filePath)
		switch {
		case err != nil:
			pipelineLog.Debugf("Thumbnail failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, err.Error())
			hasFailed = true
		case result == nil:
			pipelineLog.Debugf("Thumbnail skipped, deleting variant %d", t.ThumbVariantID)
			_ = t.VariantRepo.DeleteVariant(t.ThumbVariantID)
			thumbSkipped = true
		default:
			thumbResult = result
			hasSuccess = true
		}
	}

	// Pre-load image once if both WebP and AVIF need it to avoid double decode.
	var originImg *vipsfile.ImageHandle
	var imgInfo vipsfile.ImageInfo
	needLoad := t.WebPVariantID > 0 || t.AVIFVariantID > 0
	if needLoad {
		var err error
		originImg, imgInfo, err = vipsfile.LoadImageFromFile(filePath)
		if err != nil {
			pipelineLog.Debugf("Failed to load image: %v", err)
			if t.WebPVariantID > 0 {
				_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, fmt.Sprintf("load image: %v", err))
			}
			if t.AVIFVariantID > 0 {
				_ = t.VariantRepo.UpdateFailed(t.AVIFVariantID, fmt.Sprintf("load image: %v", err))
			}
			hasFailed = true
		} else {
			defer originImg.Close()
		}
	}

	if t.WebPVariantID > 0 {
		result, err := t.generateWebP(ctx, filePath, originImg, imgInfo)
		switch {
		case err != nil:
			pipelineLog.Debugf("WebP failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, err.Error())
			hasFailed = true
		case result == nil:
			pipelineLog.Debugf("WebP skipped, deleting variant %d", t.WebPVariantID)
			_ = t.VariantRepo.DeleteVariant(t.WebPVariantID)
			webpSkipped = true
		default:
			webpResult = result
			hasSuccess = true
		}
	}

	avifRequired := t.AVIFVariantID > 0 && t.WebPVariantID == 0
	if t.AVIFVariantID > 0 {
		result, err := t.generateAVIF(ctx, filePath, webpResult, originImg, imgInfo)
		switch {
		case err != nil:
			pipelineLog.Debugf("AVIF failed: %v", err)
			_ = t.VariantRepo.UpdateFailed(t.AVIFVariantID, err.Error())
			if avifRequired {
				hasFailed = true
			}
		case result == nil:
			pipelineLog.Debugf("AVIF skipped, deleting variant %d", t.AVIFVariantID)
			_ = t.VariantRepo.DeleteVariant(t.AVIFVariantID)
			avifSkipped = true
		default:
			avifResult = result
			hasSuccess = true
		}
	}

	if hasSuccess {
		if err := t.saveVariantResults(thumbResult, webpResult, avifResult); err != nil {
			pipelineLog.Debugf("Failed to persist variant results: %v", err)
			hasFailed = true
		}
	}

	if hasFailed {
		return fmt.Errorf("some variants failed")
	}

	_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, resolveImageVariantStatus(
		t.ThumbVariantID > 0,
		t.WebPVariantID > 0,
		t.AVIFVariantID > 0,
		thumbResult != nil,
		webpResult != nil,
		avifResult != nil,
		thumbSkipped,
		webpSkipped,
		avifSkipped,
	))
	t.deleteCacheOnTerminalState("success")
	return nil
}

// generateThumbnail 生成缩略图
func (t *ImagePipelineTask) generateThumbnail(ctx context.Context, filePath string) (*pipelineResult, error) {
	settings := t.Settings
	if settings == nil {
		return nil, fmt.Errorf("image processing settings not provided")
	}
	if !settings.ThumbnailEnabled {
		pipelineLog.Debugf("Thumbnail generation disabled")
		return nil, nil
	}

	// GIF guard: converter.go already blocks GIFs before task creation;
	// this is defense-in-depth using the stored MIME type.
	if t.MimeType == "image/gif" {
		pipelineLog.Debugf("Skipping GIF thumbnail generation")
		return nil, nil
	}

	if len(settings.ThumbnailSizes) == 0 {
		return nil, fmt.Errorf("no thumbnail sizes configured")
	}
	size := settings.ThumbnailSizes[0]

	pipelineLog.Debugf("Generating thumbnail: width=%d for variant %d", size.Width, t.ThumbVariantID)

	pg := generator.NewPathGenerator()
	thumbIdentifiers := pg.GenerateThumbnailIdentifiers(t.StoragePath, size.Width)
	thumbPath := thumbIdentifiers.StoragePath

	tmpPath, cleanupTmpPath, err := createVariantTempPath()
	if err != nil {
		return nil, fmt.Errorf("create thumbnail temp path: %w", err)
	}
	defer cleanupTmpPath()

	info, err := vipsfile.ThumbnailFileToWebP(filePath, tmpPath, size.Width, vipsfile.WebPOptions{
		Quality:         settings.ThumbnailQuality,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("thumbnail from file: %w", err)
	}

	tmpFile, fileSize, fileHash, cleanupTmp, err := stageVariantFileFromPath(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("stage thumbnail: %w", err)
	}
	defer cleanupTmp()

	if err := t.Storage.SaveWithContext(ctx, thumbPath, tmpFile); err != nil {
		return nil, fmt.Errorf("save thumbnail: %w", err)
	}

	pipelineLog.Debugf("Thumbnail saved: %s (%d bytes)", thumbPath, fileSize)

	return &pipelineResult{
		StoragePath: thumbPath,
		Width:       info.Width,
		Height:      info.Height,
		FileSize:    fileSize,
		FileHash:    fileHash,
	}, nil
}

// generateWebP 生成 WebP 原图
func (t *ImagePipelineTask) generateWebP(ctx context.Context, filePath string, originImg *vipsfile.ImageHandle, info vipsfile.ImageInfo) (*pipelineResult, error) {
	settings := t.Settings
	if settings == nil {
		return nil, fmt.Errorf("image processing settings not provided")
	}

	return t.generateWebPWithSettings(ctx, filePath, settings, originImg, info)
}

// generateWebPWithSettings 使用指定设置生成 WebP
func (t *ImagePipelineTask) generateWebPWithSettings(ctx context.Context, filePath string, settings *dbconfig.ImageProcessingSettings, originImg *vipsfile.ImageHandle, info vipsfile.ImageInfo) (*pipelineResult, error) {
	if !settings.IsFormatEnabled(models.FormatWebP) {
		pipelineLog.Debugf("WebP format disabled")
		return nil, nil
	}

	pipelineLog.Debugf("Generating WebP for variant %d", t.WebPVariantID)

	// Check dimensions from file header before full decode to avoid expensive decode of oversized images
	if settings.MaxDimension > 0 {
		if w, h, ok := readImageDimensions(filePath); ok {
			if w > settings.MaxDimension || h > settings.MaxDimension {
				pipelineLog.Debugf("Skipping WebP: image exceeds max dimension from header: %dx%d", w, h)
				return nil, nil
			}
		}
	}

	var width, height int
	if originImg == nil {
		var err error
		originImg, info, err = vipsfile.LoadImageFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("load image from file: %w", err)
		}
		defer originImg.Close()
	}
	width = info.Width
	height = info.Height
	if settings.MaxDimension > 0 {
		if width > settings.MaxDimension || height > settings.MaxDimension {
			pipelineLog.Debugf("Skipping WebP: image exceeds max dimension after load: %dx%d", width, height)
			return nil, nil
		}
	}

	complexity := detectImageComplexity(info, t.FileSize)
	adaptiveQuality := adaptiveWebPQuality(complexity, settings.WebPQuality)

	pipelineLog.Debugf("Image complexity=%d, adaptive quality=%d (base=%d)",
		complexity, adaptiveQuality, settings.WebPQuality)

	pg := generator.NewPathGenerator()
	webpIdentifiers := pg.GenerateConvertedIdentifiers(t.StoragePath, models.FormatWebP)
	originPath := webpIdentifiers.StoragePath

	tmpPath, cleanupTmpPath, err := createVariantTempPath()
	if err != nil {
		return nil, fmt.Errorf("create webp temp path: %w", err)
	}
	defer cleanupTmpPath()

	if err := originImg.SaveWebPToFile(tmpPath, vipsfile.WebPOptions{
		Quality:         adaptiveQuality,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	}); err != nil {
		return nil, fmt.Errorf("export webp: %w", err)
	}

	tmpFile, fileSize, fileHash, cleanupTmp, err := stageVariantFileFromPath(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("stage webp: %w", err)
	}
	defer cleanupTmp()

	if err := t.Storage.SaveWithContext(ctx, originPath, tmpFile); err != nil {
		return nil, fmt.Errorf("save webp: %w", err)
	}

	pipelineLog.Debugf("WebP saved: %s (%d bytes, quality=%d)", originPath, fileSize, adaptiveQuality)

	return &pipelineResult{
		StoragePath: originPath,
		Width:       width,
		Height:      height,
		FileSize:    fileSize,
		FileHash:    fileHash,
	}, nil
}

func (t *ImagePipelineTask) generateAVIF(ctx context.Context, filePath string, webpResult *pipelineResult, originImg *vipsfile.ImageHandle, info vipsfile.ImageInfo) (*pipelineResult, error) {
	settings := t.Settings
	if settings == nil {
		return nil, fmt.Errorf("image processing settings not provided")
	}
	if !settings.IsFormatEnabled(models.FormatAVIF) {
		pipelineLog.Debugf("AVIF format disabled")
		return nil, nil
	}
	if !vipsfile.SupportsAVIFEncoding() {
		pipelineLog.Debugf("AVIF encoder unavailable")
		return nil, nil
	}

	pipelineLog.Debugf("Generating AVIF for variant %d", t.AVIFVariantID)

	if settings.MaxDimension > 0 {
		if w, h, ok := readImageDimensions(filePath); ok {
			if w > settings.MaxDimension || h > settings.MaxDimension {
				pipelineLog.Debugf("Skipping AVIF: image exceeds max dimension from header: %dx%d", w, h)
				return nil, nil
			}
		}
	}

	if originImg == nil {
		var err error
		originImg, info, err = vipsfile.LoadImageFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("load image from file: %w", err)
		}
		defer originImg.Close()
	}

	if settings.MaxDimension > 0 && (info.Width > settings.MaxDimension || info.Height > settings.MaxDimension) {
		pipelineLog.Debugf("Skipping AVIF: image exceeds max dimension after load: %dx%d", info.Width, info.Height)
		return nil, nil
	}

	pg := generator.NewPathGenerator()
	avifIdentifiers := pg.GenerateConvertedIdentifiers(t.StoragePath, models.FormatAVIF)
	avifPath := avifIdentifiers.StoragePath

	tmpPath, cleanupTmpPath, err := createVariantTempPath()
	if err != nil {
		return nil, fmt.Errorf("create avif temp path: %w", err)
	}
	defer cleanupTmpPath()

	bitdepth := 8
	if settings.AVIFExperimental {
		bitdepth = 10
	}
	if err := originImg.SaveAVIFToFile(tmpPath, vipsfile.AVIFOptions{
		Quality:       settings.AVIFQuality,
		Effort:        settings.AVIFSpeed,
		StripMetadata: true,
		Bitdepth:      bitdepth,
	}); err != nil {
		return nil, fmt.Errorf("export avif: %w", err)
	}

	tmpFile, fileSize, fileHash, cleanupTmp, err := stageVariantFileFromPath(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("stage avif: %w", err)
	}
	defer cleanupTmp()

	baselineSize := t.FileSize
	if webpResult != nil && webpResult.FileSize > 0 && (baselineSize <= 0 || webpResult.FileSize < baselineSize) {
		baselineSize = webpResult.FileSize
	}
	if !shouldKeepAVIF(fileSize, baselineSize) {
		pipelineLog.Debugf("Skipping AVIF: candidate size=%d baseline=%d", fileSize, baselineSize)
		return nil, nil
	}

	if err := t.Storage.SaveWithContext(ctx, avifPath, tmpFile); err != nil {
		return nil, fmt.Errorf("save avif: %w", err)
	}

	pipelineLog.Debugf("AVIF saved: %s (%d bytes)", avifPath, fileSize)

	return &pipelineResult{
		StoragePath: avifPath,
		Width:       info.Width,
		Height:      info.Height,
		FileSize:    fileSize,
		FileHash:    fileHash,
	}, nil
}

func shouldKeepAVIF(candidateSize, baselineSize int64) bool {
	if candidateSize <= 0 {
		return false
	}
	if baselineSize <= 0 {
		return true
	}
	return candidateSize*100 < baselineSize*(100-avifMinSavingsPercent)
}

func resolveImageVariantStatus(hasThumbVariant, hasWebPVariant, hasAVIFVariant, thumbCompleted, webpCompleted, avifCompleted, thumbSkipped, webpSkipped, avifSkipped bool) models.ImageVariantStatus {
	fullCompleted := webpCompleted || avifCompleted
	allSkipped := (!hasThumbVariant || thumbSkipped) &&
		(!hasWebPVariant || webpSkipped) &&
		(!hasAVIFVariant || avifSkipped)

	switch {
	case thumbCompleted && fullCompleted:
		return models.ImageVariantStatusCompleted
	case thumbCompleted:
		return models.ImageVariantStatusThumbnailCompleted
	case fullCompleted:
		return models.ImageVariantStatusCompleted
	case allSkipped:
		return models.ImageVariantStatusNone
	default:
		return models.ImageVariantStatusNone
	}
}

// saveVariantResults 保存变体结果，任一变体写库失败时返回 error
func (t *ImagePipelineTask) saveVariantResults(thumbResult, webpResult, avifResult *pipelineResult) error {
	var firstErr error

	if t.ThumbVariantID > 0 && thumbResult != nil {
		if err := t.VariantRepo.UpdateCompleted(
			t.ThumbVariantID,
			filepath.Base(thumbResult.StoragePath),
			thumbResult.StoragePath,
			thumbResult.FileSize,
			thumbResult.FileHash,
			thumbResult.Width,
			thumbResult.Height,
		); err != nil {
			pipelineLog.Debugf("Failed to mark thumb variant %d completed: %v", t.ThumbVariantID, err)
			_ = t.VariantRepo.UpdateFailed(t.ThumbVariantID, "failed to persist result: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if t.WebPVariantID > 0 && webpResult != nil {
		if err := t.VariantRepo.UpdateCompleted(
			t.WebPVariantID,
			filepath.Base(webpResult.StoragePath),
			webpResult.StoragePath,
			webpResult.FileSize,
			webpResult.FileHash,
			webpResult.Width,
			webpResult.Height,
		); err != nil {
			pipelineLog.Debugf("Failed to mark webp variant %d completed: %v", t.WebPVariantID, err)
			_ = t.VariantRepo.UpdateFailed(t.WebPVariantID, "failed to persist result: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if t.AVIFVariantID > 0 && avifResult != nil {
		if err := t.VariantRepo.UpdateCompleted(
			t.AVIFVariantID,
			filepath.Base(avifResult.StoragePath),
			avifResult.StoragePath,
			avifResult.FileSize,
			avifResult.FileHash,
			avifResult.Width,
			avifResult.Height,
		); err != nil {
			pipelineLog.Debugf("Failed to mark avif variant %d completed: %v", t.AVIFVariantID, err)
			_ = t.VariantRepo.UpdateFailed(t.AVIFVariantID, "failed to persist result: "+err.Error())
			if firstErr == nil && t.WebPVariantID == 0 {
				firstErr = err
			}
		}
	}

	return firstErr
}

// deleteCacheOnTerminalState 终端状态删除缓存
func (t *ImagePipelineTask) deleteCacheOnTerminalState(state string) {
	if t.CacheHelper != nil && t.ImageIdentifier != "" {
		ctx := context.Background()
		if err := t.CacheHelper.DeleteCachedImage(ctx, t.ImageIdentifier); err != nil {
			pipelineLog.Debugf("Failed to delete cache for %s on %s: %v", t.ImageIdentifier, state, err)
		} else {
			pipelineLog.Debugf("Deleted cache for %s after %s", t.ImageIdentifier, state)
		}
	}
}

// cleanupAfterPipeline releases Go/C allocator state back to the OS.
// Called via defer from Execute() so it runs on success, error, and panic paths.
func (t *ImagePipelineTask) cleanupAfterPipeline() {
	runtime.GC()
	vipsfile.MallocTrim()
}

func (t *ImagePipelineTask) startProcessingHeartbeat(parent context.Context, acquiredVariants []uint) func() {
	if len(acquiredVariants) == 0 && t.ImageID == 0 {
		return func() {}
	}

	t.touchProcessingState(acquiredVariants)

	ctx, cancel := context.WithCancel(parent)
	ticker := time.NewTicker(processingHeartbeatInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.touchProcessingState(acquiredVariants)
			}
		}
	}()

	return cancel
}

func (t *ImagePipelineTask) touchProcessingState(acquiredVariants []uint) {
	if t.VariantRepo != nil {
		if err := t.VariantRepo.TouchProcessing(acquiredVariants); err != nil {
			pipelineLog.Debugf("Failed to heartbeat variants %v: %v", acquiredVariants, err)
		}
	}

	if t.ImageRepo != nil && t.ImageID > 0 {
		if err := t.ImageRepo.TouchVariantProcessingStatus(t.ImageID); err != nil {
			pipelineLog.Debugf("Failed to heartbeat image %d: %v", t.ImageID, err)
		}
	}
}

// finalize ensures variant state consistency on all exit paths.
//   - On panic: rolls back processing variants to failed.
//   - On normal exit: rolls back any variant still stuck in processing back
//     to pending (so the sweeper or a retry can pick it up).
//   - Completed/failed/skipped variants are left untouched.
func (t *ImagePipelineTask) finalize(acquiredVariants *[]uint) {
	if rec := recover(); rec != nil {
		pipelineLog.Debugf("Panic recovered: %v", rec)
		for _, id := range *acquiredVariants {
			_, _ = t.VariantRepo.UpdateStatusCAS(
				id,
				models.VariantStatusProcessing,
				models.VariantStatusFailed,
				fmt.Sprintf("panic: %v", rec),
			)
		}
		_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusFailed)
		pipelineLog.Debugf("Panic during processing for image %s; orphaned storage files (if any) can be cleaned with the 'clean' command", t.ImageIdentifier)
		return
	}

	// Normal exit: roll back any variant still in processing to pending.
	for _, id := range *acquiredVariants {
		rolledBack, _ := t.VariantRepo.UpdateStatusCAS(
			id,
			models.VariantStatusProcessing,
			models.VariantStatusPending,
			"",
		)
		if rolledBack {
			pipelineLog.Debugf("Rolled back variant %d processing->pending (pipeline did not reach terminal state)", id)
		}
	}
}
