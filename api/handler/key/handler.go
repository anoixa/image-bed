package key

import (
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/internal/repositories"
)

// Handler API Token 处理器
type Handler struct {
	repo *keys.Repository
}

// NewHandler 创建新的 Token 处理器
func NewHandler(repos *repositories.Repositories) *Handler {
	return &Handler{
		repo: repos.Keys,
	}
}
