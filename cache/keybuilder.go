package cache

import (
	"fmt"
	"strings"
)

// KeyBuilder 缓存键构建器
type KeyBuilder struct {
	prefix string
	sep    string
}

// NewKeyBuilder 创建新的键构建器
func NewKeyBuilder(prefix string) *KeyBuilder {
	return &KeyBuilder{
		prefix: prefix,
		sep:    ":",
	}
}

// WithSeparator 设置分隔符
func (kb *KeyBuilder) WithSeparator(sep string) *KeyBuilder {
	kb.sep = sep
	return kb
}

// Build 构建缓存键
func (kb *KeyBuilder) Build(parts ...string) string {
	if len(parts) == 0 {
		return kb.prefix
	}
	return kb.prefix + kb.sep + strings.Join(parts, kb.sep)
}

// BuildID 构建带 ID 的缓存键
func (kb *KeyBuilder) BuildID(id interface{}) string {
	return fmt.Sprintf("%s%s%v", kb.prefix, kb.sep, id)
}

// 预定义的 KeyBuilder 实例
var (
	// ImageMeta 图片元数据缓存
	ImageMeta = NewKeyBuilder("image_meta")

	// User 用户缓存
	User = NewKeyBuilder("user")

	// Device 设备缓存
	Device = NewKeyBuilder("device")

	// StaticToken 静态 Token 缓存
	StaticToken = NewKeyBuilder("static_token")

	// Empty 空值缓存
	Empty = NewKeyBuilder("empty")

	// Album 相册缓存
	Album = NewKeyBuilder("album")

	// AlbumList 相册列表缓存
	AlbumList = NewKeyBuilder("album_list")

	// AlbumListVersion 相册列表版本缓存
	AlbumListVersion = NewKeyBuilder("album_list_version")
)
