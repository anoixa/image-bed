package system

import (
	"os"
	"path/filepath"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/utils"
	"github.com/gin-gonic/gin"
)

// StatusResponse 系统状态响应
type StatusResponse struct {
	// 版本信息
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
	GoVersion  string `json:"go_version"`

	// 运行环境
	Environment string `json:"environment"`

	// 内存统计
	Memory MemoryStatus `json:"memory"`

	// 运行时信息
	Runtime RuntimeStatus `json:"runtime"`

	// 缓存信息
	Cache CacheStatus `json:"cache"`

	// 数据目录信息
	DataDir DirStatus `json:"data_dir"`
}

// MemoryStatus 内存状态
type MemoryStatus struct {
	HeapAllocMB   float64 `json:"heap_alloc_mb"`
	HeapAllocStr  string  `json:"heap_alloc_str"`
	HeapSysMB     float64 `json:"heap_sys_mb"`
	HeapSysStr    string  `json:"heap_sys_str"`
	HeapInUseMB   float64 `json:"heap_in_use_mb"`
	HeapInUseStr  string  `json:"heap_in_use_str"`
	StackSysMB    float64 `json:"stack_sys_mb"`
	StackSysStr   string  `json:"stack_sys_str"`
	TotalAllocMB  float64 `json:"total_alloc_mb"`
	TotalAllocStr string  `json:"total_alloc_str"`
	GCSysMB       float64 `json:"gc_sys_mb"`
	GCSysStr      string  `json:"gc_sys_str"`
	NumGC         uint32  `json:"num_gc"`
	LastGCTime    int64   `json:"last_gc_time"`
	Goroutines    int     `json:"goroutines"`
}

// RuntimeStatus 运行时状态
type RuntimeStatus struct {
	NumCPU int `json:"num_cpu"`
}

// CacheStatus 缓存状态
type CacheStatus struct {
	Provider string `json:"provider"`
	Type     string `json:"type"`
}

// DirStatus 目录状态
type DirStatus struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	TotalSize int64  `json:"total_size"`
	SizeStr   string `json:"size_str"`
}

// Handler 系统处理器
type Handler struct{}

// NewHandler 创建系统处理器
func NewHandler() *Handler {
	return &Handler{}
}

// GetStatus 获取系统状态
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
			HeapAllocMB:   memStats.HeapAllocMB,
			HeapAllocStr:  formatBytes(uint64(memStats.HeapAllocMB * 1024 * 1024)),
			HeapSysMB:     memStats.HeapSysMB,
			HeapSysStr:    formatBytes(uint64(memStats.HeapSysMB * 1024 * 1024)),
			HeapInUseMB:   memStats.HeapInUseMB,
			HeapInUseStr:  formatBytes(uint64(memStats.HeapInUseMB * 1024 * 1024)),
			StackSysMB:    memStats.StackSysMB,
			StackSysStr:   formatBytes(uint64(memStats.StackSysMB * 1024 * 1024)),
			TotalAllocMB:  0, // 将在下方计算
			TotalAllocStr: "",
			GCSysMB:       memStats.GCSysMB,
			GCSysStr:      formatBytes(uint64(memStats.GCSysMB * 1024 * 1024)),
			NumGC:         memStats.NumGC,
			LastGCTime:    memStats.LastGCTime.Unix(),
			Goroutines:    memStats.Goroutines,
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

// getGoVersion 获取 Go 版本（简化处理）
func getGoVersion() string {
	return "go1.26" // 从 go.mod 读取会更准确，这里简化处理
}

// getEnvironment 获取环境类型
func getEnvironment() string {
	if config.IsProduction() {
		return "production"
	}
	return "development"
}

// getNumCPU 获取 CPU 核心数
func getNumCPU() int {
	return utils.GetNumCPU()
}

// getDataDirInfo 获取数据目录信息
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

// countDirStats 统计目录文件数和大小
func countDirStats(dirPath string) (int, int64) {
	var fileCount int
	var totalSize int64

	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过错误
		}
		if !info.IsDir() {
			fileCount++
			totalSize += info.Size()
		}
		return nil
	})

	return fileCount, totalSize
}

// formatBytes 格式化字节数为人类可读格式
func formatBytes(bytes uint64) string {
	return utils.FormatBytes(int64(bytes))
}

// VersionResponse 版本响应
type VersionResponse struct {
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
}

// GetVersion 获取版本信息
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

// MetricsResponse 指标响应
type MetricsResponse struct {
	RequestCount      int64   `json:"request_count"`
	RequestDurationMs int64   `json:"request_duration_ms"`
	AvgDurationMs     float64 `json:"avg_duration_ms"`
}

// GetMetrics 获取指标
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
