package utils

import (
	"fmt"
)

// BuildImageURL 构建图片URL
func BuildImageURL(baseURL, identifier string) string {
	return fmt.Sprintf("%s/images/%s", baseURL, identifier)
}

// BuildThumbnailURL 构建缩略图URL
func BuildThumbnailURL(baseURL, identifier string) string {
	return fmt.Sprintf("%s/thumbnails/%s?width=600", baseURL, identifier)
}

// BuildThumbnailURLWithWidth 构建指定宽度的缩略图URL
func BuildThumbnailURLWithWidth(baseURL, identifier string, width int) string {
	return fmt.Sprintf("%s/thumbnails/%s?width=%d", baseURL, identifier, width)
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
func BuildLinkFormats(baseURL, identifier string) LinkFormats {
	url := BuildImageURL(baseURL, identifier)
	thumbnailURL := BuildThumbnailURL(baseURL, identifier)

	return LinkFormats{
		URL:              url,
		ThumbnailURL:     thumbnailURL,
		HTML:             fmt.Sprintf(`<img src="%s" alt="" />`, url),
		BBCode:           fmt.Sprintf(`[img]%s[/img]`, url),
		Markdown:         fmt.Sprintf(`![%s](%s)`, identifier, url),
		MarkdownWithLink: fmt.Sprintf(`[![%s](%s)](%s)`, identifier, thumbnailURL, url),
	}
}
