package middleware

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	requestCount    atomic.Int64
	requestDuration atomic.Int64 // in milliseconds
	uploadMetrics   uploadMetricSet
	imageMetrics    imageMetricSet
)

type uploadMetricSet struct {
	parseRequests        atomic.Int64
	parseDurationMs      atomic.Int64
	filesProcessed       atomic.Int64
	fileProcessDuration  atomic.Int64
	hashDurationMs       atomic.Int64
	storageWriteDuration atomic.Int64
	dbWriteDuration      atomic.Int64
	taskSubmitAttempts   atomic.Int64
	taskSubmitAccepted   atomic.Int64
	taskSubmitRejected   atomic.Int64
}

type imageMetricSet struct {
	metadataCacheHits   atomic.Int64
	metadataCacheMisses atomic.Int64
	dataCacheHits       atomic.Int64
	dataCacheMisses     atomic.Int64
	originalResponses   atomic.Int64
	variantResponses    atomic.Int64
	thumbnailResponses  atomic.Int64
	directRedirects     atomic.Int64
	cacheResponses      atomic.Int64
	sendfileResponses   atomic.Int64
	streamResponses     atomic.Int64
	readerResponses     atomic.Int64
}

type UploadMetrics struct {
	ParseRequests             int64   `json:"parse_requests"`
	ParseDurationMs           int64   `json:"parse_duration_ms"`
	AvgParseDurationMs        float64 `json:"avg_parse_duration_ms"`
	FilesProcessed            int64   `json:"files_processed"`
	FileProcessDurationMs     int64   `json:"file_process_duration_ms"`
	AvgFileProcessDurationMs  float64 `json:"avg_file_process_duration_ms"`
	HashDurationMs            int64   `json:"hash_duration_ms"`
	AvgHashDurationMs         float64 `json:"avg_hash_duration_ms"`
	StorageWriteDurationMs    int64   `json:"storage_write_duration_ms"`
	AvgStorageWriteDurationMs float64 `json:"avg_storage_write_duration_ms"`
	DBWriteDurationMs         int64   `json:"db_write_duration_ms"`
	AvgDBWriteDurationMs      float64 `json:"avg_db_write_duration_ms"`
	TaskSubmitAttempts        int64   `json:"task_submit_attempts"`
	TaskSubmitAccepted        int64   `json:"task_submit_accepted"`
	TaskSubmitRejected        int64   `json:"task_submit_rejected"`
}

type ImageMetrics struct {
	MetadataCacheHits   int64 `json:"metadata_cache_hits"`
	MetadataCacheMisses int64 `json:"metadata_cache_misses"`
	DataCacheHits       int64 `json:"data_cache_hits"`
	DataCacheMisses     int64 `json:"data_cache_misses"`
	OriginalResponses   int64 `json:"original_responses"`
	VariantResponses    int64 `json:"variant_responses"`
	ThumbnailResponses  int64 `json:"thumbnail_responses"`
	DirectRedirects     int64 `json:"direct_redirects"`
	CacheResponses      int64 `json:"cache_responses"`
	SendfileResponses   int64 `json:"sendfile_responses"`
	StreamResponses     int64 `json:"stream_responses"`
	ReaderResponses     int64 `json:"reader_responses"`
}

// Metrics 基础监控指标中间件
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 响应完成后记录指标
		c.Writer.Header().Set("X-Request-Count", fmt.Sprintf("%d", requestCount.Load()))

		c.Next()

		duration := time.Since(startTime)
		requestDuration.Add(duration.Milliseconds())
		requestCount.Add(1)
	}
}

func RecordUploadParseDuration(duration time.Duration) {
	uploadMetrics.parseRequests.Add(1)
	uploadMetrics.parseDurationMs.Add(duration.Milliseconds())
}

func RecordUploadFileProcessed(duration time.Duration) {
	uploadMetrics.filesProcessed.Add(1)
	uploadMetrics.fileProcessDuration.Add(duration.Milliseconds())
}

func RecordUploadHashDuration(duration time.Duration) {
	uploadMetrics.hashDurationMs.Add(duration.Milliseconds())
}

func RecordUploadStorageWriteDuration(duration time.Duration) {
	uploadMetrics.storageWriteDuration.Add(duration.Milliseconds())
}

func RecordUploadDBWriteDuration(duration time.Duration) {
	uploadMetrics.dbWriteDuration.Add(duration.Milliseconds())
}

func RecordUploadTaskSubmit(accepted bool) {
	uploadMetrics.taskSubmitAttempts.Add(1)
	if accepted {
		uploadMetrics.taskSubmitAccepted.Add(1)
		return
	}
	uploadMetrics.taskSubmitRejected.Add(1)
}

func RecordImageMetadataCacheHit() {
	imageMetrics.metadataCacheHits.Add(1)
}

func RecordImageMetadataCacheMiss() {
	imageMetrics.metadataCacheMisses.Add(1)
}

func RecordImageDataCacheHit() {
	imageMetrics.dataCacheHits.Add(1)
}

func RecordImageDataCacheMiss() {
	imageMetrics.dataCacheMisses.Add(1)
}

func RecordImageOriginalResponse() {
	imageMetrics.originalResponses.Add(1)
}

func RecordImageVariantResponse() {
	imageMetrics.variantResponses.Add(1)
}

func RecordImageThumbnailResponse() {
	imageMetrics.thumbnailResponses.Add(1)
}

func RecordImageDirectRedirect() {
	imageMetrics.directRedirects.Add(1)
}

func RecordImageCacheResponse() {
	imageMetrics.cacheResponses.Add(1)
}

func RecordImageSendfileResponse() {
	imageMetrics.sendfileResponses.Add(1)
}

func RecordImageStreamResponse() {
	imageMetrics.streamResponses.Add(1)
}

func RecordImageReaderResponse() {
	imageMetrics.readerResponses.Add(1)
}

func avgDuration(total, count int64) float64 {
	if count <= 0 {
		return 0
	}
	return float64(total) / float64(count)
}

// GetMetrics 获取当前指标
func GetMetrics() map[string]any {
	count := requestCount.Load()
	duration := requestDuration.Load()
	return map[string]any{
		"request_count":       count,
		"request_duration_ms": duration,
		"avg_duration_ms": func() float64 {
			if count > 0 {
				return float64(duration) / float64(count)
			}
			return 0
		}(),
		"upload": UploadMetrics{
			ParseRequests:             uploadMetrics.parseRequests.Load(),
			ParseDurationMs:           uploadMetrics.parseDurationMs.Load(),
			AvgParseDurationMs:        avgDuration(uploadMetrics.parseDurationMs.Load(), uploadMetrics.parseRequests.Load()),
			FilesProcessed:            uploadMetrics.filesProcessed.Load(),
			FileProcessDurationMs:     uploadMetrics.fileProcessDuration.Load(),
			AvgFileProcessDurationMs:  avgDuration(uploadMetrics.fileProcessDuration.Load(), uploadMetrics.filesProcessed.Load()),
			HashDurationMs:            uploadMetrics.hashDurationMs.Load(),
			AvgHashDurationMs:         avgDuration(uploadMetrics.hashDurationMs.Load(), uploadMetrics.filesProcessed.Load()),
			StorageWriteDurationMs:    uploadMetrics.storageWriteDuration.Load(),
			AvgStorageWriteDurationMs: avgDuration(uploadMetrics.storageWriteDuration.Load(), uploadMetrics.filesProcessed.Load()),
			DBWriteDurationMs:         uploadMetrics.dbWriteDuration.Load(),
			AvgDBWriteDurationMs:      avgDuration(uploadMetrics.dbWriteDuration.Load(), uploadMetrics.filesProcessed.Load()),
			TaskSubmitAttempts:        uploadMetrics.taskSubmitAttempts.Load(),
			TaskSubmitAccepted:        uploadMetrics.taskSubmitAccepted.Load(),
			TaskSubmitRejected:        uploadMetrics.taskSubmitRejected.Load(),
		},
		"image_delivery": ImageMetrics{
			MetadataCacheHits:   imageMetrics.metadataCacheHits.Load(),
			MetadataCacheMisses: imageMetrics.metadataCacheMisses.Load(),
			DataCacheHits:       imageMetrics.dataCacheHits.Load(),
			DataCacheMisses:     imageMetrics.dataCacheMisses.Load(),
			OriginalResponses:   imageMetrics.originalResponses.Load(),
			VariantResponses:    imageMetrics.variantResponses.Load(),
			ThumbnailResponses:  imageMetrics.thumbnailResponses.Load(),
			DirectRedirects:     imageMetrics.directRedirects.Load(),
			CacheResponses:      imageMetrics.cacheResponses.Load(),
			SendfileResponses:   imageMetrics.sendfileResponses.Load(),
			StreamResponses:     imageMetrics.streamResponses.Load(),
			ReaderResponses:     imageMetrics.readerResponses.Load(),
		},
	}
}
