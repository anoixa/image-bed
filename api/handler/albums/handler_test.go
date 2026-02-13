package albums

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	return router
}

// --- 测试请求 DTO 绑定 ---

// TestCreateAlbumRequest_Binding 测试创建相册请求绑定
func TestCreateAlbumRequest_Binding(t *testing.T) {
	router := setupTestRouter(t)
	router.POST("/test", func(c *gin.Context) {
		var req createAlbumRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondSuccess(c, req)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid request",
			body: map[string]interface{}{
				"name":        "Test Album",
				"description": "Test Description",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "missing name",
			body: map[string]interface{}{
				"description": "Test Description",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "name too long",
			body: map[string]interface{}{
				"name":        string(make([]byte, 101)),
				"description": "Test",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "description too long",
			body: map[string]interface{}{
				"name":        "Test",
				"description": string(make([]byte, 256)),
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty body",
			body: map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestUpdateAlbumRequest_Binding 测试更新相册请求绑定
func TestUpdateAlbumRequest_Binding(t *testing.T) {
	router := setupTestRouter(t)
	router.PUT("/test", func(c *gin.Context) {
		var req UpdateAlbumRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondSuccess(c, req)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid request",
			body: map[string]interface{}{
				"name":        "Updated Album",
				"description": "Updated Description",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "missing name",
			body: map[string]interface{}{
				"description": "Test",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty description allowed",
			body: map[string]interface{}{
				"name": "Test Album",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPut, "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestAddImagesToAlbumRequest_Binding 测试添加图片请求绑定
func TestAddImagesToAlbumRequest_Binding(t *testing.T) {
	router := setupTestRouter(t)
	router.POST("/test", func(c *gin.Context) {
		var req AddImagesToAlbumRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondSuccess(c, req)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid request",
			body: map[string]interface{}{
				"image_ids": []uint{1, 2, 3},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "empty image_ids",
			body: map[string]interface{}{
				"image_ids": []uint{},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing image_ids",
			body: map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestListAlbumsRequest_Binding 测试列表相册请求绑定
func TestListAlbumsRequest_Binding(t *testing.T) {
	router := setupTestRouter(t)
	router.GET("/test", func(c *gin.Context) {
		var req ListAlbumsRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondSuccess(c, req)
	})

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{
			name:       "valid request",
			query:      "?page=1&limit=10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "page too small",
			query:      "?page=0&limit=10",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit too large",
			query:      "?page=1&limit=200",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing params",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// --- 测试路径参数解析 ---

// TestAlbumIDParam_Parsing 测试相册ID参数解析
func TestAlbumIDParam_Parsing(t *testing.T) {
	router := setupTestRouter(t)
	router.GET("/albums/:id", func(c *gin.Context) {
		id := c.Param("id")
		c.JSON(http.StatusOK, gin.H{"id": id})
	})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "valid numeric id",
			path:       "/albums/123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid string id",
			path:       "/albums/abc",
			wantStatus: http.StatusOK, // gin 允许任意字符串作为参数
		},
		{
			name:       "zero id",
			path:       "/albums/0",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestImageIDParam_Parsing 测试图片ID参数解析
func TestImageIDParam_Parsing(t *testing.T) {
	router := setupTestRouter(t)
	router.DELETE("/albums/:id/images/:imageId", func(c *gin.Context) {
		albumID := c.Param("id")
		imageID := c.Param("imageId")
		c.JSON(http.StatusOK, gin.H{
			"album_id": albumID,
			"image_id": imageID,
		})
	})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "valid ids",
			path:       "/albums/123/images/456",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid album id",
			path:       "/albums/abc/images/456",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid image id",
			path:       "/albums/123/images/abc",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
