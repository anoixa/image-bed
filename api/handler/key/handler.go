package key

import (
	"github.com/anoixa/image-bed/database/repo/keys"
)

// Handler API Token 处理器
type Handler struct {
	repo *keys.Repository
}

// NewHandler 创建新的 Token 处理器
func NewHandler(repo *keys.Repository) *Handler {
	return &Handler{
		repo: repo,
	}
}
