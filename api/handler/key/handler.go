package key

import (
	"github.com/anoixa/image-bed/internal/auth"
)

// Handler API Token 处理器
type Handler struct {
	svc *auth.KeyService
}

// NewHandler 创建新的 Token 处理器
func NewHandler(svc *auth.KeyService) *Handler {
	return &Handler{
		svc: svc,
	}
}
