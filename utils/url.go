package utils

import (
	"fmt"

	"github.com/anoixa/image-bed/config"
)

// BuildImageURL  Base URL for images
func BuildImageURL(identifier string) string {
	cfg := config.Get()
	return fmt.Sprintf("%s/images/%s", cfg.Server.BaseURL, identifier)
}
