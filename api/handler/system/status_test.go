package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/internal/worker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusReturnsRuntimeAndVipsMemoryFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	handler := NewHandler()
	router.GET("/system/status", handler.GetStatus)

	req := httptest.NewRequest(http.MethodGet, "/system/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload StatusResponse
	require.NoError(t, json.Unmarshal(dataBytes, &payload))

	assert.GreaterOrEqual(t, payload.Memory.TotalAllocMB, payload.Memory.HeapAllocMB)
	assert.GreaterOrEqual(t, payload.Memory.RSSMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.HeapIdleMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.HeapReleasedMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.RssAnonMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.RssFileMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.VipsMemMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.VipsMemHighMB, float64(0))
	assert.GreaterOrEqual(t, payload.Memory.VipsAllocs, int64(0))
	assert.GreaterOrEqual(t, payload.Memory.VipsOpenFiles, int64(0))
	assert.Equal(t, runtime.Version(), payload.GoVersion)
	assert.GreaterOrEqual(t, payload.Worker.Submitted, uint64(0))
	assert.GreaterOrEqual(t, payload.Worker.Executed, uint64(0))
	assert.GreaterOrEqual(t, payload.Worker.Failed, uint64(0))
	assert.GreaterOrEqual(t, payload.Worker.InFlightTasks, 0)
	assert.GreaterOrEqual(t, payload.Worker.InFlightVariants, 0)
	assert.GreaterOrEqual(t, payload.Sweeper.Runs, uint64(0))
	assert.GreaterOrEqual(t, payload.Sweeper.Errors, uint64(0))
}

func TestGetMetricsIncludesWorkerAndSweeperSections(t *testing.T) {
	gin.SetMode(gin.TestMode)

	worker.InitGlobalPool(1, 1)
	defer worker.StopGlobalPool()

	router := gin.New()
	handler := NewHandler()
	router.GET("/system/metrics", handler.GetMetrics)

	req := httptest.NewRequest(http.MethodGet, "/system/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload struct {
		RequestCount      int64                    `json:"request_count"`
		RequestDurationMs int64                    `json:"request_duration_ms"`
		AvgDurationMs     float64                  `json:"avg_duration_ms"`
		Upload            middleware.UploadMetrics `json:"upload"`
		ImageDelivery     middleware.ImageMetrics  `json:"image_delivery"`
		Worker            WorkerStatus             `json:"worker"`
		Sweeper           worker.SweeperStats      `json:"sweeper"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))

	assert.GreaterOrEqual(t, payload.RequestCount, int64(0))
	assert.GreaterOrEqual(t, payload.Upload.ParseRequests, int64(0))
	assert.GreaterOrEqual(t, payload.ImageDelivery.MetadataCacheHits, int64(0))
	assert.GreaterOrEqual(t, payload.Worker.QueueCap, 1)
	assert.GreaterOrEqual(t, payload.Worker.WorkerCount, 1)
	assert.GreaterOrEqual(t, payload.Sweeper.Runs, uint64(0))
}
