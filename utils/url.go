package utils

import (
	"fmt"

	"github.com/anoixa/image-bed/config"
)

// BuildImageURL 构建图片URL
func BuildImageURL(identifier string) string {
	cfg := config.Get()
	return fmt.Sprintf("%s/images/%s", cfg.BaseURL(), identifier)
}

// BuildThumbnailURL 构建缩略图URL
func BuildThumbnailURL(identifier string) string {
	cfg := config.Get()
	return fmt.Sprintf("%s/thumbnails/%s?width=600", cfg.BaseURL(), identifier)
}

// BuildThumbnailURLWithWidth 构建指定宽度的缩略图URL
func BuildThumbnailURLWithWidth(identifier string, width int) string {
	cfg := config.Get()
	return fmt.Sprintf("%s/thumbnails/%s?width=%d", cfg.BaseURL(), identifier, width)
}

// LinkFormats 包含各种格式的图片链接
type LinkFormats struct {
	URL              string `json:"url"`
	ThumbnailURL     string `json:"thumbnail_url"`
	HTML             string `json:"html"`
	BBCode           string `json:"bbcode"`
	Markdown         string `json:"markdown"`
	MarkdownWithLink string `json:"markdown_with_link"`
}

// BuildLinkFormats 构建各种格式的图片链接
func BuildLinkFormats(identifier string) LinkFormats {
	url := BuildImageURL(identifier)
	thumbnailURL := BuildThumbnailURL(identifier)

	return LinkFormats{
		URL:              url,
		ThumbnailURL:     thumbnailURL,
		HTML:             fmt.Sprintf(`<img src="%s" alt="" />`, url),
		BBCode:           fmt.Sprintf(`[img]%s[/img]`, url),
		Markdown:         fmt.Sprintf(`![%s](%s)`, identifier, url),
		MarkdownWithLink: fmt.Sprintf(`[![%s](%s)](%s)`, identifier, thumbnailURL, url),
	}
}
