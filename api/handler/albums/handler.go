package albums

import (
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/internal/repositories"
)

// Handler 相册处理器
type Handler struct {
	repo *albums.Repository
}

// NewHandler 创建新的相册处理器
func NewHandler(repos *repositories.Repositories) *Handler {
	return &Handler{
		repo: repos.Albums,
	}
}
