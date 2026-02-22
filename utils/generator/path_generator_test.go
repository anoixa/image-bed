package generator

import (
	"strings"
	"testing"
	"time"
)

func TestPathGenerator_GenerateOriginalIdentifier(t *testing.T) {
	pg := NewPathGenerator()
	uploadTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		fileHash string
		ext      string
		want     string
	}{
		{
			name:     "jpg file",
			fileHash: "a1b2c3d4e5f6g7h8i9j0k1l2",
			ext:      ".jpg",
			want:     "original/2024/01/15/a1b2c3d4e5f6.jpg",
		},
		{
			name:     "png file",
			fileHash: "abcdef123456",
			ext:      ".png",
			want:     "original/2024/01/15/abcdef123456.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateOriginalIdentifier(tt.fileHash, tt.ext, uploadTime)
			if got != tt.want {
				t.Errorf("GenerateOriginalIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_GenerateThumbnailIdentifier(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name               string
		originalIdentifier string
		width              int
		want               string
	}{
		{
			name:               "from new format",
			originalIdentifier: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			width:              300,
			want:               "thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp",
		},
		{
			name:               "from old format (root)",
			originalIdentifier: "a1b2c3d4e5f6.jpg",
			width:              600,
			want:               "thumbnails/a1b2c3d4e5f6_600.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateThumbnailIdentifier(tt.originalIdentifier, tt.width)
			if got != tt.want {
				t.Errorf("GenerateThumbnailIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_GenerateConvertedIdentifier(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name               string
		originalIdentifier string
		format             string
		want               string
	}{
		{
			name:               "webp format",
			originalIdentifier: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			format:             "webp",
			want:               "converted/webp/2024/01/15/a1b2c3d4e5f6.webp",
		},
		{
			name:               "avif format",
			originalIdentifier: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			format:             "avif",
			want:               "converted/avif/2024/01/15/a1b2c3d4e5f6.avif",
		},
		{
			name:               "from old format",
			originalIdentifier: "a1b2c3d4e5f6.jpg",
			format:             "webp",
			want:               "converted/webp/a1b2c3d4e5f6.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateConvertedIdentifier(tt.originalIdentifier, tt.format)
			if got != tt.want {
				t.Errorf("GenerateConvertedIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_extractHash(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{
			name:       "new format original",
			identifier: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			want:       "a1b2c3d4e5f6",
		},
		{
			name:       "new format thumbnail",
			identifier: "thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp",
			want:       "a1b2c3d4e5f6",
		},
		{
			name:       "old format",
			identifier: "a1b2c3d4e5f6.jpg",
			want:       "a1b2c3d4e5f6",
		},
		{
			name:       "hash with underscore (not size)",
			identifier: "original/2024/01/15/abc_def_ghi.jpg",
			want:       "abc_def_ghi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.extractHash(tt.identifier)
			if got != tt.want {
				t.Errorf("extractHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_IsHierarchicalPath(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name       string
		identifier string
		want       bool
	}{
		{
			name:       "new format",
			identifier: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			want:       true,
		},
		{
			name:       "old format",
			identifier: "a1b2c3d4e5f6.jpg",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.IsHierarchicalPath(tt.identifier)
			if got != tt.want {
				t.Errorf("IsHierarchicalPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_HashUniqueness(t *testing.T) {
	pg := NewPathGenerator()
	uploadTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// 测试相同哈希不同时间
	hash := "a1b2c3d4e5f6g7h8i9j0k1l2"
	id1 := pg.GenerateOriginalIdentifier(hash, ".jpg", uploadTime)
	id2 := pg.GenerateOriginalIdentifier(hash, ".jpg", uploadTime.Add(24*time.Hour))

	if id1 == id2 {
		t.Error("相同哈希不同日期应该生成不同路径")
	}

	if !strings.Contains(id1, "01/15") || !strings.Contains(id2, "01/16") {
		t.Error("路径应该包含正确的日期")
	}
}
