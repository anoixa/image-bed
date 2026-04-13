package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/anoixa/image-bed/api/common"
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
}
