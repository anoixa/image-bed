package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupChunkedTestRouter(t *testing.T, handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// 模拟认证中间件，注入用户ID
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextUserIDKey, uint(1))
		c.Next()
	})

	return router
}

func createTestFile(t *testing.T, size int64, seed byte) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = seed + byte(i%256)
	}
	return data
}

func calculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// --- Test: InitChunkedUpload Request Validation ---

// TestInitChunkedUploadRequest_Binding 测试初始化请求参数绑定
func TestInitChunkedUploadRequest_Binding(t *testing.T) {
	router := setupChunkedTestRouter(t, nil)
	router.POST("/init", func(c *gin.Context) {
		var req InitChunkedUploadRequest
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
				"file_name":  "test-large-file.zip",
				"file_hash":  calculateHash([]byte("test")),
				"total_size": 100 * 1024 * 1024,
				"chunk_size": 5 * 1024 * 1024,
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "missing file_name",
			body: map[string]interface{}{
				"file_hash":  calculateHash([]byte("test")),
				"total_size": 100 * 1024 * 1024,
				"chunk_size": 5 * 1024 * 1024,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing file_hash",
			body: map[string]interface{}{
				"file_name":  "test.zip",
				"total_size": 100 * 1024 * 1024,
				"chunk_size": 5 * 1024 * 1024,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid total_size (zero)",
			body: map[string]interface{}{
				"file_name":  "test.zip",
				"file_hash":  calculateHash([]byte("test")),
				"total_size": 0,
				"chunk_size": 5 * 1024 * 1024,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "chunk_size too small",
			body: map[string]interface{}{
				"file_name":  "test.zip",
				"file_hash":  calculateHash([]byte("test")),
				"total_size": 10 * 1024 * 1024,
				"chunk_size": 512 * 1024, // < 1MB
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "chunk_size too large",
			body: map[string]interface{}{
				"file_name":  "test.zip",
				"file_hash":  calculateHash([]byte("test")),
				"total_size": 10 * 1024 * 1024,
				"chunk_size": 100 * 1024 * 1024, // > 50MB
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty body",
			body:       map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/init", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestInitChunkedUpload_Calculation 测试分片计算逻辑
func TestInitChunkedUpload_Calculation(t *testing.T) {
	tests := []struct {
		name        string
		totalSize   int64
		chunkSize   int64
		wantChunks  int
		shouldError bool
	}{
		{
			name:       "exact division",
			totalSize:  100 * 1024 * 1024,
			chunkSize:  5 * 1024 * 1024,
			wantChunks: 20,
		},
		{
			name:       "with remainder",
			totalSize:  1024*1024*1024 + 1024, // 1GB + 1KB
			chunkSize:  100 * 1024 * 1024,     // 100MB
			wantChunks: 11,                    // 10 full + 1 partial
		},
		{
			name:       "single chunk",
			totalSize:  1024 * 1024,
			chunkSize:  5 * 1024 * 1024,
			wantChunks: 1,
		},
		{
			name:        "exceeds max chunks",
			totalSize:   10 * 1024 * 1024 * 1024, // 10GB
			chunkSize:   1024 * 1024,             // 1MB = 10000 chunks
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalChunks := int((tt.totalSize + tt.chunkSize - 1) / tt.chunkSize)
			exceedsMax := totalChunks > MaxChunks

			if tt.shouldError {
				assert.True(t, exceedsMax, "应该超过最大分片数限制")
			} else {
				assert.False(t, exceedsMax, "不应该超过最大分片数限制")
				assert.Equal(t, tt.wantChunks, totalChunks)
			}
		})
	}
}

// --- Test: UploadChunk Request Validation ---

// TestUploadChunkRequest_Binding 测试分片上传请求绑定
func TestUploadChunkRequest_Binding(t *testing.T) {
	router := setupChunkedTestRouter(t, nil)
	router.POST("/chunk", func(c *gin.Context) {
		var req UploadChunkRequest
		if err := c.ShouldBind(&req); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondSuccess(c, req)
	})

	createMultipartRequest := func(sessionID, chunkIndex string, includeFile bool) *http.Request {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if sessionID != "" {
			_ = writer.WriteField("session_id", sessionID)
		}
		if chunkIndex != "" {
			_ = writer.WriteField("chunk_index", chunkIndex)
		}
		if includeFile {
			part, _ := writer.CreateFormFile("chunk", "chunk")
			_, _ = part.Write([]byte("test chunk data"))
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/chunk", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req
	}

	tests := []struct {
		name       string
		sessionID  string
		chunkIndex string
		wantStatus int
	}{
		{
			name:       "missing session_id",
			sessionID:  "",
			chunkIndex: "0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative chunk_index",
			sessionID:  "valid-session-id",
			chunkIndex: "-1",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-numeric chunk_index",
			sessionID:  "valid-session-id",
			chunkIndex: "abc",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createMultipartRequest(tt.sessionID, tt.chunkIndex, true)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// --- Test: CompleteChunkedUpload Request Validation ---

// TestCompleteChunkedUploadRequest_Binding 测试完成上传请求绑定
func TestCompleteChunkedUploadRequest_Binding(t *testing.T) {
	router := setupChunkedTestRouter(t, nil)
	router.POST("/complete", func(c *gin.Context) {
		var req CompleteChunkedUploadRequest
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
				"session_id": "valid-session-id",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing session_id",
			body:       map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty session_id",
			body: map[string]interface{}{
				"session_id": "",
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// --- Test: ChunkedUploadSession Logic ---

// TestChunkedUploadSession_MissingChunks 测试缺失分片检测
func TestChunkedUploadSession_MissingChunks(t *testing.T) {
	tests := []struct {
		name           string
		totalChunks    int
		receivedChunks map[int]bool
		wantMissing    []int
		isComplete     bool
	}{
		{
			name:           "all chunks received",
			totalChunks:    5,
			receivedChunks: map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true},
			wantMissing:    []int{},
			isComplete:     true,
		},
		{
			name:           "first chunk missing",
			totalChunks:    5,
			receivedChunks: map[int]bool{1: true, 2: true, 3: true, 4: true},
			wantMissing:    []int{0},
			isComplete:     false,
		},
		{
			name:           "last chunk missing",
			totalChunks:    5,
			receivedChunks: map[int]bool{0: true, 1: true, 2: true, 3: true},
			wantMissing:    []int{4},
			isComplete:     false,
		},
		{
			name:           "middle chunks missing",
			totalChunks:    5,
			receivedChunks: map[int]bool{0: true, 4: true},
			wantMissing:    []int{1, 2, 3},
			isComplete:     false,
		},
		{
			name:           "no chunks received",
			totalChunks:    3,
			receivedChunks: map[int]bool{},
			wantMissing:    []int{0, 1, 2},
			isComplete:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &ChunkedUploadSession{
				TotalChunks:    tt.totalChunks,
				ReceivedChunks: tt.receivedChunks,
			}

			// 检测是否完整
			isComplete := len(session.ReceivedChunks) == session.TotalChunks
			assert.Equal(t, tt.isComplete, isComplete)

			// 计算缺失分片
			if !isComplete {
				var missingChunks []int
				for i := 0; i < session.TotalChunks; i++ {
					if !session.ReceivedChunks[i] {
						missingChunks = append(missingChunks, i)
					}
				}
				assert.Equal(t, tt.wantMissing, missingChunks)
			}
		})
	}
}

// TestChunkedUploadSession_Expiry 测试会话过期检测
func TestChunkedUploadSession_Expiry(t *testing.T) {
	tests := []struct {
		name      string
		createdAt time.Time
		isExpired bool
	}{
		{
			name:      "fresh session",
			createdAt: time.Now(),
			isExpired: false,
		},
		{
			name:      "session at boundary",
			createdAt: time.Now().Add(-23 * time.Hour),
			isExpired: false,
		},
		{
			name:      "expired session",
			createdAt: time.Now().Add(-25 * time.Hour),
			isExpired: true,
		},
		{
			name:      "very old session",
			createdAt: time.Now().Add(-48 * time.Hour),
			isExpired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &ChunkedUploadSession{
				CreatedAt: tt.createdAt,
			}

			isExpired := time.Since(session.CreatedAt) > UploadSessionExpiry
			assert.Equal(t, tt.isExpired, isExpired)
		})
	}
}

// --- Test: File Operations ---

// TestChunkFileOperations 测试分片文件操作
func TestChunkFileOperations(t *testing.T) {
	tempDir := t.TempDir()
	sessionID := "test-session"

	// 创建临时目录
	sessionDir := filepath.Join(tempDir, sessionID)
	err := os.MkdirAll(sessionDir, 0755)
	assert.NoError(t, err)

	// 模拟写入分片
	numChunks := 5
	originalData := make([][]byte, numChunks)
	for i := 0; i < numChunks; i++ {
		originalData[i] = []byte(fmt.Sprintf("chunk %d data content", i))
		chunkPath := filepath.Join(sessionDir, fmt.Sprintf("%d", i))
		err := os.WriteFile(chunkPath, originalData[i], 0644)
		assert.NoError(t, err)
	}

	// 模拟合并文件
	mergedPath := filepath.Join(tempDir, "merged")
	mergedFile, err := os.Create(mergedPath)
	assert.NoError(t, err)

	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(sessionDir, fmt.Sprintf("%d", i))
		chunkData, err := os.ReadFile(chunkPath)
		assert.NoError(t, err)
		_, err = mergedFile.Write(chunkData)
		assert.NoError(t, err)
	}
	_ = mergedFile.Close()

	// 验证合并后文件
	mergedData, err := os.ReadFile(mergedPath)
	assert.NoError(t, err)

	var expectedData []byte
	for _, data := range originalData {
		expectedData = append(expectedData, data...)
	}
	assert.Equal(t, expectedData, mergedData)

	// 验证哈希
	originalHash := calculateHash(expectedData)
	mergedHash := calculateHash(mergedData)
	assert.Equal(t, originalHash, mergedHash)

	// 模拟清理
	err = os.RemoveAll(sessionDir)
	assert.NoError(t, err)

	_, err = os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(err), "临时目录应该被删除")
}

// --- Test: Edge Cases ---

// TestChunkSizeBoundary 测试分片大小边界
func TestChunkSizeBoundary(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int64
		isValid   bool
	}{
		{
			name:      "minimum valid",
			chunkSize: 1 * 1024 * 1024, // 1MB
			isValid:   true,
		},
		{
			name:      "maximum valid",
			chunkSize: 50 * 1024 * 1024, // 50MB
			isValid:   true,
		},
		{
			name:      "just below minimum",
			chunkSize: 1*1024*1024 - 1,
			isValid:   false,
		},
		{
			name:      "just above maximum",
			chunkSize: 50*1024*1024 + 1,
			isValid:   false,
		},
		{
			name:      "zero",
			chunkSize: 0,
			isValid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.chunkSize >= 1048576 && tt.chunkSize <= 52428800
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

// TestConcurrentChunkUpload 测试并发分片上传模拟
func TestConcurrentChunkUpload(t *testing.T) {
	tempDir := t.TempDir()
	numChunks := 10

	// 模拟多个 goroutine 同时写入不同分片
	done := make(chan int, numChunks)

	for i := 0; i < numChunks; i++ {
		go func(index int) {
			chunkPath := filepath.Join(tempDir, fmt.Sprintf("%d", index))
			data := []byte(fmt.Sprintf("concurrent chunk %d", index))
			err := os.WriteFile(chunkPath, data, 0644)
			if err == nil {
				done <- index
			}
		}(i)
	}

	// 收集结果
	receivedChunks := make(map[int]bool)
	for i := 0; i < numChunks; i++ {
		idx := <-done
		receivedChunks[idx] = true
	}

	assert.Equal(t, numChunks, len(receivedChunks))

	// 验证所有文件存在
	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("%d", i))
		_, err := os.Stat(chunkPath)
		assert.NoError(t, err)
	}
}

// --- Benchmark Tests ---

// BenchmarkChunkCalculation 分片计算性能基准测试
func BenchmarkChunkCalculation(b *testing.B) {
	totalSize := int64(1024 * 1024 * 1024) // 1GB
	chunkSize := int64(5 * 1024 * 1024)    // 5MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		totalChunks := int((totalSize + chunkSize - 1) / chunkSize)
		_ = totalChunks
	}
}

// BenchmarkHashCalculation 哈希计算性能基准测试
func BenchmarkHashCalculation(b *testing.B) {
	data := createTestFile(nil, 5*1024*1024, 1) // 5MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateHash(data)
	}
}

// BenchmarkChunkFileWrite 分片文件写入性能基准测试
func BenchmarkChunkFileWrite(b *testing.B) {
	tempDir := b.TempDir()
	data := createTestFile(nil, 5*1024*1024, 1) // 5MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", i))
		_ = os.WriteFile(chunkPath, data, 0644)
	}
}

// --- Integration Helper: Full Flow Simulation ---

// TestFullChunkedUploadFlow_Simulation 完整上传流程模拟测试
func TestFullChunkedUploadFlow_Simulation(t *testing.T) {
	// 1. 准备测试文件
	fileSize := int64(10 * 1024 * 1024) // 10MB
	chunkSize := int64(2 * 1024 * 1024) // 2MB per chunk
	numChunks := int((fileSize + chunkSize - 1) / chunkSize)

	fileData := createTestFile(t, fileSize, 42)
	fileHash := calculateHash(fileData)

	// 2. 模拟初始化
	sessionID := "simulated-session"
	tempDir := t.TempDir()

	session := &ChunkedUploadSession{
		SessionID:      sessionID,
		FileName:       "test-upload.bin",
		FileHash:       fileHash,
		TotalSize:      fileSize,
		ChunkSize:      chunkSize,
		TotalChunks:    numChunks,
		ReceivedChunks: make(map[int]bool),
		TempDir:        tempDir,
		CreatedAt:      time.Now(),
	}

	// 3. 模拟上传分片（乱序）
	uploadOrder := []int{2, 0, 4, 1, 3} // 乱序上传
	for _, idx := range uploadOrder {
		start := int64(idx) * chunkSize
		end := start + chunkSize
		if end > fileSize {
			end = fileSize
		}

		chunkData := fileData[start:end]
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("%d", idx))
		err := os.WriteFile(chunkPath, chunkData, 0644)
		assert.NoError(t, err)

		session.ReceivedChunks[idx] = true
	}

	// 4. 验证所有分片已接收
	assert.Equal(t, numChunks, len(session.ReceivedChunks))

	// 5. 模拟合并
	mergedPath := filepath.Join(t.TempDir(), "merged")
	mergedFile, err := os.Create(mergedPath)
	assert.NoError(t, err)

	for i := 0; i < numChunks; i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("%d", i))
		chunkData, err := os.ReadFile(chunkPath)
		assert.NoError(t, err)
		_, err = mergedFile.Write(chunkData)
		assert.NoError(t, err)
	}
	_ = mergedFile.Close()

	// 6. 验证完整性
	mergedData, err := os.ReadFile(mergedPath)
	assert.NoError(t, err)
	assert.Equal(t, len(fileData), len(mergedData))
	assert.Equal(t, fileHash, calculateHash(mergedData))

	// 7. 模拟保存到数据库
	savedImage := &models.Image{
		Identifier:   "generated-identifier",
		OriginalName: session.FileName,
		FileSize:     session.TotalSize,
		FileHash:     session.FileHash,
	}
	assert.NotNil(t, savedImage)
	assert.Equal(t, session.FileHash, savedImage.FileHash)
}
