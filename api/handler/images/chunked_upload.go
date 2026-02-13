package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
	"github.com/anoixa/image-bed/utils/async"
	"github.com/anoixa/image-bed/utils/validator"
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
)

// ChunkedUploadSession 分片上传会话
type ChunkedUploadSession struct {
	SessionID      string
	FileName       string
	FileHash       string
	TotalSize      int64
	ChunkSize      int64
	TotalChunks    int
	ReceivedChunks map[int]bool
	StorageDriver  string
	UserID         uint
	TempDir        string
	CreatedAt      time.Time
}

// 内存中的上传会话管理
var (
	sessions   = make(map[string]*ChunkedUploadSession)
	sessionsMu sync.RWMutex
)

// InitChunkedUploadRequest 初始化分片上传请求
type InitChunkedUploadRequest struct {
	FileName  string `json:"file_name" binding:"required"`
	FileHash  string `json:"file_hash" binding:"required"` // 客户端预计算的哈希
	TotalSize int64  `json:"total_size" binding:"required,min=1"`
	ChunkSize int64  `json:"chunk_size" binding:"required,min=1048576,max=52428800"` // 1MB - 50MB
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

	// 检查文件是否已存在（秒传）
	existingImage, err := images.GetImageByHash(req.FileHash)
	if err == nil {
		// 文件已存在，直接返回
		common.RespondSuccess(c, gin.H{
			"session_id":     "",
			"total_chunks":   0,
			"chunk_size":     0,
			"instant_upload": true,
			"identifier":     existingImage.Identifier,
			"url":            utils.BuildImageURL(existingImage.Identifier),
		})
		return
	}

	userID := c.GetUint(middleware.ContextUserIDKey)
	sessionID := uuid.New().String()

	// 创建临时目录
	tempDir := filepath.Join("./data", "temp", sessionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create temp directory")
		return
	}

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

	// 检查分片索引
	if req.ChunkIndex >= session.TotalChunks {
		common.RespondError(c, http.StatusBadRequest, "Invalid chunk index")
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("chunk")
	if err != nil {
		common.RespondError(c, http.StatusBadRequest, "Chunk file is required")
		return
	}

	// 验证分片大小
	if file.Size > session.ChunkSize+1024 { // 允许 1KB 误差
		common.RespondError(c, http.StatusBadRequest, "Chunk size exceeds expected size")
		return
	}

	// 保存分片到临时文件
	src, err := file.Open()
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to open chunk file")
		return
	}
	defer src.Close()

	chunkPath := filepath.Join(session.TempDir, strconv.Itoa(req.ChunkIndex))
	dst, err := os.Create(chunkPath)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to create chunk file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
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

	// 检查是否所有分片都已上传
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

	// 从 map 中移除会话
	delete(sessions, req.SessionID)
	sessionsMu.Unlock()

	// 异步合并分片并保存
	go func() {
		ctx := context.Background()
		if err := h.processChunkedUpload(ctx, session); err != nil {
			log.Printf("Failed to process chunked upload: %v", err)
		}
		// 清理临时目录
		os.RemoveAll(session.TempDir)
	}()

	common.RespondSuccess(c, gin.H{
		"message":    "Upload completed, processing in background",
		"session_id": session.SessionID,
	})
}

// processChunkedUpload 合并分片并保存
func (h *Handler) processChunkedUpload(ctx context.Context, session *ChunkedUploadSession) error {
	// 合并所有分片
	mergedFile := filepath.Join(session.TempDir, "merged")
	outFile, err := os.Create(mergedFile)
	if err != nil {
		return fmt.Errorf("failed to create merged file: %w", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(outFile, hasher)

	for i := 0; i < session.TotalChunks; i++ {
		chunkPath := filepath.Join(session.TempDir, strconv.Itoa(i))
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open chunk %d: %w", i, err)
		}

		if _, err := io.Copy(writer, chunkFile); err != nil {
			chunkFile.Close()
			outFile.Close()
			return fmt.Errorf("failed to copy chunk %d: %w", i, err)
		}
		chunkFile.Close()
	}

	outFile.Close()

	// 验证哈希
	fileHash := hex.EncodeToString(hasher.Sum(nil))
	if fileHash != session.FileHash {
		return fmt.Errorf("file hash mismatch: expected %s, got %s", session.FileHash, fileHash)
	}

	// 读取文件进行验证
	fileBytes, err := os.ReadFile(mergedFile)
	if err != nil {
		return fmt.Errorf("failed to read merged file: %w", err)
	}

	isImage, mimeType := validator.IsImageBytes(fileBytes)
	if !isImage {
		return fmt.Errorf("uploaded file is not a valid image")
	}

	// 获取存储提供者
	storageProvider, err := h.storageFactory.Get("")
	if err != nil {
		return fmt.Errorf("failed to get storage: %w", err)
	}

	cfg := config.Get()
	driverToSave := cfg.Server.StorageConfig.Type

	// 生成唯一标识符
	ext := filepath.Ext(session.FileName)
	identifier := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), fileHash[:16], ext)

	// 保存到存储
	if err := storageProvider.SaveWithContext(ctx, identifier, bytes.NewReader(fileBytes)); err != nil {
		return fmt.Errorf("failed to save file to storage: %w", err)
	}

	// 创建数据库记录
	newImage := &models.Image{
		Identifier:    identifier,
		OriginalName:  session.FileName,
		FileSize:      int64(len(fileBytes)),
		MimeType:      mimeType,
		StorageDriver: driverToSave,
		FileHash:      fileHash,
		UserID:        session.UserID,
	}

	if err := images.SaveImage(newImage); err != nil {
		storageProvider.DeleteWithContext(ctx, identifier)
		return fmt.Errorf("failed to save image metadata: %w", err)
	}

	// 异步提取图片尺寸
	async.ExtractImageDimensionsAsync(identifier, driverToSave)

	log.Printf("Chunked upload completed: %s -> %s", session.FileName, identifier)
	return nil
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
	})
}

// CleanupExpiredSessions 清理过期会话
func CleanupExpiredSessions() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	now := time.Now()
	for id, session := range sessions {
		if now.Sub(session.CreatedAt) > UploadSessionExpiry {
			delete(sessions, id)
			os.RemoveAll(session.TempDir)
			log.Printf("Cleaned up expired upload session: %s", id)
		}
	}
}
