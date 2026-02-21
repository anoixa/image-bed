package images

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/internal/image"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// DefaultChunkSize 默认分片大小：5MB
	DefaultChunkSize = 5 * 1024 * 1024
	// MaxChunks 最大分片数
	MaxChunks = 1000
	// UploadSessionExpiry 上传会话过期时间
	UploadSessionExpiry = 24 * time.Hour
	// MaxUploadSessions 最大上传会话数
	MaxUploadSessions = 1000
)

// ChunkedUploadSession 分片上传会话
type ChunkedUploadSession struct {
	SessionID       string
	FileName        string
	FileHash        string
	TotalSize       int64
	ChunkSize       int64
	TotalChunks     int
	ReceivedChunks  map[int]bool
	StorageConfigID uint
	UserID          uint
	TempDir         string
	CreatedAt       time.Time
	IsProcessing    bool
}

var (
	sessions   = make(map[string]*ChunkedUploadSession)
	sessionsMu sync.RWMutex
)

// InitChunkedUploadRequest 初始化分片上传请求
type InitChunkedUploadRequest struct {
	FileName  string `json:"file_name" binding:"required"`
	FileHash  string `json:"file_hash" binding:"required"`
	TotalSize int64  `json:"total_size" binding:"required,min=1"`
	ChunkSize int64  `json:"chunk_size" binding:"required,min=1048576,max=52428800"`
}

// InitChunkedUploadResponse 初始化分片上传响应
type InitChunkedUploadResponse struct {
	SessionID   string `json:"session_id"`
	TotalChunks int    `json:"total_chunks"`
	ChunkSize   int64  `json:"chunk_size"`
}

// UploadChunkRequest 上传分片请求
type UploadChunkRequest struct {
	SessionID  string `form:"session_id" binding:"required"`
	ChunkIndex int    `form:"chunk_index" binding:"required,min=0"`
}

// CompleteChunkedUploadRequest 完成分片上传请求
type CompleteChunkedUploadRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// InitChunkedUpload 初始化分片上传
func (h *Handler) InitChunkedUpload(c *gin.Context) {
	var req InitChunkedUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 计算总分片数
	totalChunks := int((req.TotalSize + req.ChunkSize - 1) / req.ChunkSize)
	if totalChunks > MaxChunks {
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Total chunks exceed maximum allowed: %d", MaxChunks))
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)

	// 调用 Service 初始化上传
	ctx := c.Request.Context()
	result, err := h.imageService.InitChunkedUpload(ctx, req.FileName, req.FileHash, req.TotalSize, req.ChunkSize, userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 秒传检查
	if result.InstantUpload {
		common.RespondSuccess(c, gin.H{
			"session_id":     "",
			"total_chunks":   0,
			"chunk_size":     0,
			"instant_upload": true,
			"identifier":     result.Identifier,
			"links":          result.Links,
		})
		return
	}

	// 创建临时目录
	sessionID := uuid.New().String()
	tempDir := filepath.Join("./data", "temp", sessionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create temp directory")
		return
	}

	// 创建会话
	session := &ChunkedUploadSession{
		SessionID:      sessionID,
		FileName:       req.FileName,
		FileHash:       req.FileHash,
		TotalSize:      req.TotalSize,
		ChunkSize:      req.ChunkSize,
		TotalChunks:    totalChunks,
		ReceivedChunks: make(map[int]bool),
		UserID:         userID,
		TempDir:        tempDir,
		CreatedAt:      time.Now(),
	}

	sessionsMu.Lock()
	if len(sessions) >= MaxUploadSessions {
		sessionsMu.Unlock()
		_ = os.RemoveAll(tempDir)
		common.RespondError(c, http.StatusServiceUnavailable, "Server is busy, too many upload sessions")
		return
	}
	sessions[sessionID] = session
	sessionsMu.Unlock()

	common.RespondSuccess(c, InitChunkedUploadResponse{
		SessionID:   sessionID,
		TotalChunks: totalChunks,
		ChunkSize:   req.ChunkSize,
	})
}

// UploadChunk 上传单个分片
func (h *Handler) UploadChunk(c *gin.Context) {
	var req UploadChunkRequest
	if err := c.ShouldBind(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	sessionsMu.RLock()
	session, exists := sessions[req.SessionID]
	sessionsMu.RUnlock()

	if !exists {
		common.RespondError(c, http.StatusNotFound, "Upload session not found or expired")
		return
	}

	if req.ChunkIndex >= session.TotalChunks {
		common.RespondError(c, http.StatusBadRequest, "Invalid chunk index")
		return
	}

	file, err := c.FormFile("chunk")
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Chunk file is required")
		return
	}

	if file.Size > session.ChunkSize+1024 {
		common.RespondError(c, http.StatusBadRequest, "Chunk size exceeds expected size")
		return
	}

	// 保存分片
	src, err := file.Open()
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to open chunk file")
		return
	}
	defer func() { _ = src.Close() }()

	chunkPath := filepath.Join(session.TempDir, strconv.Itoa(req.ChunkIndex))
	dst, err := os.Create(chunkPath)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create chunk file")
		return
	}
	defer func() { _ = dst.Close() }()

	if _, err := dst.ReadFrom(src); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to save chunk")
		return
	}

	sessionsMu.Lock()
	session.ReceivedChunks[req.ChunkIndex] = true
	receivedCount := len(session.ReceivedChunks)
	sessionsMu.Unlock()

	common.RespondSuccess(c, gin.H{
		"chunk_index":    req.ChunkIndex,
		"received_count": receivedCount,
		"total_chunks":   session.TotalChunks,
	})
}

// CompleteChunkedUpload 完成分片上传并合并
func (h *Handler) CompleteChunkedUpload(c *gin.Context) {
	var req CompleteChunkedUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	sessionsMu.Lock()
	session, exists := sessions[req.SessionID]
	if !exists {
		sessionsMu.Unlock()
		common.RespondError(c, http.StatusNotFound, "Upload session not found or expired")
		return
	}

	if session.IsProcessing {
		sessionsMu.Unlock()
		common.RespondError(c, http.StatusConflict, "Upload session is already being processed")
		return
	}

	if len(session.ReceivedChunks) != session.TotalChunks {
		missingChunks := make([]int, 0)
		for i := 0; i < session.TotalChunks; i++ {
			if !session.ReceivedChunks[i] {
				missingChunks = append(missingChunks, i)
			}
		}
		sessionsMu.Unlock()
		common.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Not all chunks uploaded, missing: %v", missingChunks))
		return
	}

	session.IsProcessing = true
	sessionsMu.Unlock()

	// 异步处理合并
	go h.processChunkedUploadAsync(req.SessionID, session)

	common.RespondSuccess(c, gin.H{
		"message":    "Upload completed, processing in background",
		"session_id": session.SessionID,
	})
}

// toServiceSession 将 Handler 的 session 转换为 Service 的 session
func toServiceSession(s *ChunkedUploadSession) *image.ChunkedUploadSession {
	return &image.ChunkedUploadSession{
		SessionID:       s.SessionID,
		FileName:        s.FileName,
		FileHash:        s.FileHash,
		TotalSize:       s.TotalSize,
		ChunkSize:       s.ChunkSize,
		TotalChunks:     s.TotalChunks,
		ReceivedChunks:  s.ReceivedChunks,
		StorageConfigID: s.StorageConfigID,
		UserID:          s.UserID,
		TempDir:         s.TempDir,
		CreatedAt:       s.CreatedAt,
		IsProcessing:    s.IsProcessing,
	}
}

// processChunkedUploadAsync 异步处理分片上传
func (h *Handler) processChunkedUploadAsync(sessionID string, session *ChunkedUploadSession) {
	defer func() {
		sessionsMu.Lock()
		delete(sessions, sessionID)
		sessionsMu.Unlock()
		_ = os.RemoveAll(session.TempDir)
	}()

	ctx := context.Background()
	_, err := h.imageService.ProcessChunkedUpload(ctx, toServiceSession(session))
	if err != nil {
		// 错误处理已在服务层记录
		_ = err
	}
}

// GetChunkedUploadStatus 获取分片上传状态
func (h *Handler) GetChunkedUploadStatus(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.RespondError(c, http.StatusBadRequest, "session_id is required")
		return
	}

	sessionsMu.RLock()
	session, exists := sessions[sessionID]
	sessionsMu.RUnlock()

	if !exists {
		common.RespondError(c, http.StatusNotFound, "Upload session not found or expired")
		return
	}

	receivedChunks := make([]int, 0, len(session.ReceivedChunks))
	for idx := range session.ReceivedChunks {
		receivedChunks = append(receivedChunks, idx)
	}

	common.RespondSuccess(c, gin.H{
		"session_id":      session.SessionID,
		"file_name":       session.FileName,
		"total_size":      session.TotalSize,
		"total_chunks":    session.TotalChunks,
		"received_chunks": len(session.ReceivedChunks),
		"received_list":   receivedChunks,
		"is_processing":   session.IsProcessing,
	})
}

// CleanupExpiredSessions 清理过期的分片上传会话
func CleanupExpiredSessions() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	now := time.Now()
	expiredCount := 0

	for sessionID, session := range sessions {
		if now.Sub(session.CreatedAt) > UploadSessionExpiry {
			if session.TempDir != "" {
				_ = os.RemoveAll(session.TempDir)
			}
			delete(sessions, sessionID)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		// 清理日志
	}
}
