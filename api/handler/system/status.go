package system

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

type StatusResponse struct {
	Version     string        `json:"version"`
	CommitHash  string        `json:"commit_hash"`
	GoVersion   string        `json:"go_version"`
	Environment string        `json:"environment"`
	Memory      MemoryStatus  `json:"memory"`
	Runtime     RuntimeStatus `json:"runtime"`
	Cache       CacheStatus   `json:"cache"`
	DataDir     DirStatus     `json:"data_dir"`
}

type MemoryStatus struct {
	HeapAllocMB    float64 `json:"heap_alloc_mb"`
	HeapAllocStr   string  `json:"heap_alloc_str"`
	HeapSysMB      float64 `json:"heap_sys_mb"`
	HeapSysStr     string  `json:"heap_sys_str"`
	HeapInUseMB    float64 `json:"heap_in_use_mb"`
	HeapInUseStr   string  `json:"heap_in_use_str"`
	StackSysMB     float64 `json:"stack_sys_mb"`
	StackSysStr    string  `json:"stack_sys_str"`
	RSSMB          float64 `json:"rss_mb"`
	RSSStr         string  `json:"rss_str"`
	TotalAllocMB   float64 `json:"total_alloc_mb"`
	TotalAllocStr  string  `json:"total_alloc_str"`
	GCSysMB        float64 `json:"gc_sys_mb"`
	GCSysStr       string  `json:"gc_sys_str"`
	NumGC          uint32  `json:"num_gc"`
	LastGCTime     int64   `json:"last_gc_time"`
	Goroutines     int     `json:"goroutines"`
	VipsMemMB      float64 `json:"vips_mem_mb"`
	VipsMemStr     string  `json:"vips_mem_str"`
	VipsMemHighMB  float64 `json:"vips_mem_high_mb"`
	VipsMemHighStr string  `json:"vips_mem_high_str"`
	VipsAllocs     int64   `json:"vips_allocs"`
	VipsOpenFiles  int64   `json:"vips_open_files"`
}

type RuntimeStatus struct {
	NumCPU int `json:"num_cpu"`
}

type CacheStatus struct {
	Provider string `json:"provider"`
	Type     string `json:"type"`
}

type DirStatus struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	TotalSize int64  `json:"total_size"`
	SizeStr   string `json:"size_str"`
}

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

// GetStatus
// @Summary      Get system status
// @Description  Get detailed system runtime information including version, memory stats, goroutines, GC count, and cache info
// @Tags         system
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response{data=StatusResponse}  "System status"
// @Router       /system/status [get]
func (h *Handler) GetStatus(c *gin.Context) {
	// 获取内存统计
	memStats := utils.GetMemoryStats()

	// 获取缓存提供者信息
	cacheProvider := cache.GetDefault()
	cacheName := "none"
	cacheType := "none"
	if cacheProvider != nil {
		cacheName = cacheProvider.Name()
		cacheType = cacheName
	}

	// 获取数据目录信息
	dataDirInfo := getDataDirInfo()

	// 构建响应
	response := StatusResponse{
		Version:     config.Version,
		CommitHash:  config.CommitHash,
		GoVersion:   getGoVersion(),
		Environment: getEnvironment(),
		Memory: MemoryStatus{
			HeapAllocMB:    memStats.HeapAllocMB,
			HeapAllocStr:   formatBytes(uint64(memStats.HeapAllocMB * 1024 * 1024)),
			HeapSysMB:      memStats.HeapSysMB,
			HeapSysStr:     formatBytes(uint64(memStats.HeapSysMB * 1024 * 1024)),
			HeapInUseMB:    memStats.HeapInUseMB,
			HeapInUseStr:   formatBytes(uint64(memStats.HeapInUseMB * 1024 * 1024)),
			StackSysMB:     memStats.StackSysMB,
			StackSysStr:    formatBytes(uint64(memStats.StackSysMB * 1024 * 1024)),
			RSSMB:          memStats.RSSMB,
			RSSStr:         formatBytes(uint64(memStats.RSSMB * 1024 * 1024)),
			TotalAllocMB:   memStats.TotalAllocMB,
			TotalAllocStr:  formatBytes(uint64(memStats.TotalAllocMB * 1024 * 1024)),
			GCSysMB:        memStats.GCSysMB,
			GCSysStr:       formatBytes(uint64(memStats.GCSysMB * 1024 * 1024)),
			NumGC:          memStats.NumGC,
			LastGCTime:     memStats.LastGCTime.Unix(),
			Goroutines:     memStats.Goroutines,
			VipsMemMB:      memStats.VipsMemMB,
			VipsMemStr:     formatBytes(uint64(memStats.VipsMemMB * 1024 * 1024)),
			VipsMemHighMB:  memStats.VipsMemHighMB,
			VipsMemHighStr: formatBytes(uint64(memStats.VipsMemHighMB * 1024 * 1024)),
			VipsAllocs:     memStats.VipsAllocs,
			VipsOpenFiles:  memStats.VipsOpenFiles,
		},
		Runtime: RuntimeStatus{
			NumCPU: getNumCPU(),
		},
		Cache: CacheStatus{
			Provider: cacheName,
			Type:     cacheType,
		},
		DataDir: dataDirInfo,
	}

	common.RespondSuccess(c, response)
}

func getGoVersion() string {
	return runtime.Version()
}

func getEnvironment() string {
	if config.IsProduction() {
		return "production"
	}
	return "development"
}

func getNumCPU() int {
	return utils.GetNumCPU()
}

func getDataDirInfo() DirStatus {
	dataPath := "./data"
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return DirStatus{
			Path:      dataPath,
			FileCount: 0,
			TotalSize: 0,
			SizeStr:   "0 B",
		}
	}

	fileCount, totalSize := countDirStats(dataPath)

	return DirStatus{
		Path:      dataPath,
		FileCount: fileCount,
		TotalSize: totalSize,
		SizeStr:   formatBytes(uint64(totalSize)),
	}
}

func countDirStats(dirPath string) (int, int64) {
	var fileCount int
	var totalSize int64

	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			fileCount++
			totalSize += info.Size()
		}
		return nil
	})

	return fileCount, totalSize
}

func formatBytes(bytes uint64) string {
	return utils.FormatBytes(int64(bytes))
}

type VersionResponse struct {
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
}

// GetVersion
// @Summary      Get version
// @Description  Get application version and commit hash
// @Tags         system
// @Accept       json
// @Produce      json
// @Success      200  {object}  common.Response{data=VersionResponse}  "Version info"
// @Router       /system/version [get]
func (h *Handler) GetVersion(c *gin.Context) {
	common.RespondSuccess(c, VersionResponse{
		Version:    config.Version,
		CommitHash: config.CommitHash,
	})
}

type MetricsResponse struct {
	RequestCount      int64   `json:"request_count"`
	RequestDurationMs int64   `json:"request_duration_ms"`
	AvgDurationMs     float64 `json:"avg_duration_ms"`
}

// GetMetrics
// @Summary      Get metrics
// @Description  Get application metrics and statistics
// @Tags         system
// @Accept       json
// @Produce      json
// @Success      200  {object}  MetricsResponse  "Metrics data"
// @Router       /system/metrics [get]
func (h *Handler) GetMetrics(c *gin.Context) {
	c.JSON(200, middleware.GetMetrics())
}
