package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/anoixa/image-bed/utils"
	"github.com/davidbyttow/govips/v2/vips"
)

// readWithLimit 读取流并检查大小限制
// 多读 1 字节探测是否超限，避免 io.LimitReader 的静默截断
func readWithLimit(r io.Reader, limit int64) ([]byte, error) {
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds max size %d bytes", limit)
	}
	return data, nil
}

// ImagePipelineTask 统一图片处理任务
// 合并 WebP 转换和缩略图生成为单一流水线，减少 IO
// 优先保证缩略图生成，失败直接返回
type ImagePipelineTask struct {
	VariantID       uint
	ImageID         uint
	StoragePath     string
	ImageIdentifier string
	Storage         storage.Provider
	ConfigManager   *dbconfig.Manager
	VariantRepo     VariantRepository
	ImageRepo       ImageRepository
	CacheHelper     *cache.Helper
}

// Execute 执行任务
func (t *ImagePipelineTask) Execute() {
	defer t.recovery()

	ctx := context.Background()

	// 1. CAS 获取 processing 状态（复用现有重试机制）
	utils.LogIfDevf("[Pipeline] Attempting CAS: variant %d pending->processing", t.VariantID)
	acquired, err := t.VariantRepo.UpdateStatusCAS(
		t.VariantID,
		models.VariantStatusPending,
		models.VariantStatusProcessing,
		"",
	)
	if err != nil {
		utils.LogIfDevf("[Pipeline] CAS error for variant %d: %v", t.VariantID, err)
		return
	}
	if !acquired {
		utils.LogIfDevf("[Pipeline] CAS failed for variant %d (not in pending state)", t.VariantID)
		return
	}
	utils.LogIfDevf("[Pipeline] CAS success: variant %d is now processing", t.VariantID)

	// 2. 信号量控制并发（内存保护）
	semaphore := GetGlobalSemaphore()
	if err := semaphore.Acquire(ctx); err != nil {
		utils.LogIfDevf("[Pipeline] Failed to acquire semaphore: %v", err)
		t.handleFailure(fmt.Errorf("acquire semaphore: %w", err))
		return
	}
	defer semaphore.Release()

	// 3. 执行处理流水线
	utils.LogIfDevf("[Pipeline] Starting processing for variant %d, image=%s", t.VariantID, t.StoragePath)
	if err := t.runPipeline(ctx); err != nil {
		utils.LogIfDevf("[Pipeline] Processing failed for variant %d: %v", t.VariantID, err)
		t.handleFailure(err)
		return
	}

	utils.LogIfDevf("[Pipeline] Processing success for variant %d", t.VariantID)
}

// runPipeline 执行处理流水线
// 流程：读取文件 -> 生成缩略图（优先） -> 生成 WebP 原图
func (t *ImagePipelineTask) runPipeline(ctx context.Context) error {
	// 获取全局配置中的上传大小限制
	maxSize := int64(config.Get().UploadMaxSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024 // 默认 50MB
	}

	// 1. 获取网络流
	stream, err := t.Storage.GetWithContext(ctx, t.StoragePath)
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}
	// 尝试关闭流（如果实现了 io.Closer）
	if closer, ok := stream.(io.Closer); ok {
		defer closer.Close()
	}

	// 2. 读取到内存（带大小检查）
	fileBytes, err := readWithLimit(stream, maxSize)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	utils.LogIfDevf("[Pipeline] Loaded %d bytes for variant %d", len(fileBytes), t.VariantID)

	// ========== 优先：生成缩略图（用户最先看到）==========
	thumbResult, err := t.generateThumbnail(ctx, fileBytes)
	if err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}

	// ========== 其次：生成 WebP 原图 ==========
	originResult, err := t.generateWebP(ctx, fileBytes)
	if err != nil {
		// WebP 失败不阻塞，只记录日志
		utils.LogIfDevf("[Pipeline] WebP conversion failed for variant %d: %v", t.VariantID, err)
		originResult = nil
	}

	runtime.GC() // 触发 GC 回收内存

	// 更新成功状态
	t.handleSuccess(thumbResult, originResult)
	return nil
}

// pipelineResult 处理结果
type pipelineResult struct {
	StoragePath string
	Width       int
	Height      int
	FileSize    int64
}

// generateThumbnail 生成缩略图
// 宽度 600px，高度自适应（等比缩放）
func (t *ImagePipelineTask) generateThumbnail(ctx context.Context, fileBytes []byte) (*pipelineResult, error) {
	thumbSettings, err := t.ConfigManager.GetThumbnailSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get thumbnail settings: %w", err)
	}
	if !thumbSettings.Enabled {
		utils.LogIfDevf("[Pipeline] Thumbnail generation disabled")
		return nil, nil
	}

	// 取第一个尺寸配置（现在只有 600px）
	if len(thumbSettings.Sizes) == 0 {
		return nil, fmt.Errorf("no thumbnail sizes configured")
	}
	size := thumbSettings.Sizes[0]

	utils.LogIfDevf("[Pipeline] Generating thumbnail: width=%d for variant %d", size.Width, t.VariantID)

	// 使用 NewThumbnailFromBuffer，高度传 0 表示等比缩放
	thumbImg, err := vips.NewThumbnailFromBuffer(
		fileBytes,
		size.Width,
		0, // 高度 0 = 不限，等比缩放
		vips.InterestingCentre,
	)
	if err != nil {
		return nil, fmt.Errorf("thumbnail from buffer: %w", err)
	}
	defer thumbImg.Close()

	thumbWebp, _, err := thumbImg.ExportWebp(&vips.WebpExportParams{
		Quality:         80,
		Lossless:        false,
		ReductionEffort: 4,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export thumbnail webp: %w", err)
	}

	width := thumbImg.Width()
	height := thumbImg.Height()

	// 修正扩展名：去掉原扩展名，添加 .thumb.webp
	base := strings.TrimSuffix(t.StoragePath, filepath.Ext(t.StoragePath))
	thumbPath := base + ".thumb.webp"

	if err := t.Storage.SaveWithContext(ctx, thumbPath, bytes.NewReader(thumbWebp)); err != nil {
		return nil, fmt.Errorf("save thumbnail: %w", err)
	}

	utils.LogIfDevf("[Pipeline] Thumbnail saved: %s (%d bytes)", thumbPath, len(thumbWebp))

	return &pipelineResult{
		StoragePath: thumbPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(thumbWebp)),
	}, nil
}

// generateWebP 生成 WebP 原图
func (t *ImagePipelineTask) generateWebP(ctx context.Context, fileBytes []byte) (*pipelineResult, error) {
	settings, err := t.ConfigManager.GetConversionSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get conversion settings: %w", err)
	}

	return t.generateWebPWithSettings(ctx, fileBytes, settings)
}

// generateWebPWithSettings 使用指定设置生成 WebP
func (t *ImagePipelineTask) generateWebPWithSettings(ctx context.Context, fileBytes []byte, settings *dbconfig.ConversionSettings) (*pipelineResult, error) {
	if !t.isFormatEnabled(settings, models.FormatWebP) {
		utils.LogIfDevf("[Pipeline] WebP format disabled")
		return nil, nil
	}

	utils.LogIfDevf("[Pipeline] Generating WebP for variant %d", t.VariantID)

	originImg, err := vips.NewImageFromBuffer(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("load image from buffer: %w", err)
	}
	defer originImg.Close()

	// 检查尺寸限制
	width := originImg.Width()
	height := originImg.Height()
	if settings.MaxDimension > 0 {
		if width > settings.MaxDimension || height > settings.MaxDimension {
			return nil, fmt.Errorf("image exceeds max dimension: %dx%d", width, height)
		}
	}

	originWebp, _, err := originImg.ExportWebp(&vips.WebpExportParams{
		Quality:         settings.WebPQuality,
		Lossless:        false,
		ReductionEffort: settings.WebPEffort,
		StripMetadata:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("export webp: %w", err)
	}

	// 修正扩展名
	base := strings.TrimSuffix(t.StoragePath, filepath.Ext(t.StoragePath))
	originPath := base + ".webp"

	if err := t.Storage.SaveWithContext(ctx, originPath, bytes.NewReader(originWebp)); err != nil {
		return nil, fmt.Errorf("save webp: %w", err)
	}

	utils.LogIfDevf("[Pipeline] WebP saved: %s (%d bytes)", originPath, len(originWebp))

	return &pipelineResult{
		StoragePath: originPath,
		Width:       width,
		Height:      height,
		FileSize:    int64(len(originWebp)),
	}, nil
}

// isFormatEnabled 检查格式是否启用
func (t *ImagePipelineTask) isFormatEnabled(settings *dbconfig.ConversionSettings, format string) bool {
	for _, f := range settings.EnabledFormats {
		if f == format {
			return true
		}
	}
	return false
}

// handleFailure 处理失败
func (t *ImagePipelineTask) handleFailure(err error) {
	_ = t.VariantRepo.UpdateFailed(t.VariantID, err.Error(), true)
	t.deleteCacheOnTerminalState("failed")
}

// handleSuccess 处理成功
func (t *ImagePipelineTask) handleSuccess(thumbResult, originResult *pipelineResult) {
	// 更新缩略图变体
	if thumbResult != nil {
		_ = t.VariantRepo.UpdateCompleted(
			t.VariantID,
			filepath.Base(thumbResult.StoragePath),
			thumbResult.StoragePath,
			thumbResult.FileSize,
			thumbResult.Width,
			thumbResult.Height,
		)
	}

	// 更新图片状态
	_ = t.ImageRepo.UpdateVariantStatus(t.ImageID, models.ImageVariantStatusCompleted)

	t.deleteCacheOnTerminalState("success")
}

// deleteCacheOnTerminalState 终端状态删除缓存
func (t *ImagePipelineTask) deleteCacheOnTerminalState(state string) {
	if t.CacheHelper != nil && t.ImageIdentifier != "" {
		ctx := context.Background()
		if err := t.CacheHelper.DeleteCachedImage(ctx, t.ImageIdentifier); err != nil {
			utils.LogIfDevf("[Pipeline] Failed to delete cache for %s on %s: %v", t.ImageIdentifier, state, err)
		} else {
			utils.LogIfDevf("[Pipeline] Deleted cache for %s after %s", t.ImageIdentifier, state)
		}
	}
}

// recovery 恢复 panic
func (t *ImagePipelineTask) recovery() {
	if rec := recover(); rec != nil {
		utils.LogIfDevf("[Pipeline] Panic recovered: %v", rec)
		_, _ = t.VariantRepo.UpdateStatusCAS(
			t.VariantID,
			models.VariantStatusProcessing,
			models.VariantStatusFailed,
			fmt.Sprintf("panic: %v", rec),
		)
	}
}
