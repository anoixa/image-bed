package utils

import (
	"fmt"
	"net/url"
	"strings"
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

// ExtractCookieDomain 从 URL 中提取有效的 Cookie Domain
// Cookie Domain 只能是主机名（如 "example.com" 或 "localhost"）
// 不能包含协议（如 "http://"）或端口（如 ":8081"）
func ExtractCookieDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// 如果已经是纯域名（不包含协议），尝试直接返回
	// 但也需要移除端口
	if !strings.Contains(rawURL, "://") {
		host := rawURL
		// 移除端口号
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			// 确保这是端口（只有数字）
			port := host[idx+1:]
			isPort := true
			for _, c := range port {
				if c < '0' || c > '9' {
					isPort = false
					break
				}
			}
			if isPort {
				host = host[:idx]
			}
		}
		return host
	}

	// 解析 URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	// 获取主机名（移除端口号）
	host := parsed.Hostname()
	return host
}
