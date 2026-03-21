package key

import (
	"github.com/anoixa/image-bed/internal/auth"
)

type Handler struct {
	svc *auth.KeyService
}

func NewHandler(svc *auth.KeyService) *Handler {
	return &Handler{
		svc: svc,
	}
}
