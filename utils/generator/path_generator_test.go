package generator

import (
	"strings"
	"testing"
	"time"
)

func TestPathGenerator_GenerateOriginalIdentifiers(t *testing.T) {
	pg := NewPathGenerator()
	uploadTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name            string
		fileHash        string
		ext             string
		wantIdentifier  string
		wantStoragePath string
	}{
		{
			name:            "jpg file",
			fileHash:        "a1b2c3d4e5f6g7h8i9j0k1l2",
			ext:             ".jpg",
			wantIdentifier:  "a1b2c3d4e5f6",
			wantStoragePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
		},
		{
			name:            "png file",
			fileHash:        "abcdef123456",
			ext:             ".png",
			wantIdentifier:  "abcdef123456",
			wantStoragePath: "original/2024/01/15/abcdef123456.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateOriginalIdentifiers(tt.fileHash, tt.ext, uploadTime)
			if got.Identifier != tt.wantIdentifier {
				t.Errorf("GenerateOriginalIdentifiers() Identifier = %v, want %v", got.Identifier, tt.wantIdentifier)
			}
			if got.StoragePath != tt.wantStoragePath {
				t.Errorf("GenerateOriginalIdentifiers() StoragePath = %v, want %v", got.StoragePath, tt.wantStoragePath)
			}
		})
	}
}

func TestPathGenerator_GenerateThumbnailIdentifiers(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name                string
		originalStoragePath string
		width               int
		wantIdentifier      string
		wantStoragePath     string
	}{
		{
			name:                "from new format",
			originalStoragePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			width:               300,
			wantIdentifier:      "a1b2c3d4e5f6_300",
			wantStoragePath:     "thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp",
		},
		{
			name:                "600 width",
			originalStoragePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			width:               600,
			wantIdentifier:      "a1b2c3d4e5f6_600",
			wantStoragePath:     "thumbnails/2024/01/15/a1b2c3d4e5f6_600.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateThumbnailIdentifiers(tt.originalStoragePath, tt.width)
			if got.Identifier != tt.wantIdentifier {
				t.Errorf("GenerateThumbnailIdentifiers() Identifier = %v, want %v", got.Identifier, tt.wantIdentifier)
			}
			if got.StoragePath != tt.wantStoragePath {
				t.Errorf("GenerateThumbnailIdentifiers() StoragePath = %v, want %v", got.StoragePath, tt.wantStoragePath)
			}
		})
	}
}

func TestPathGenerator_GenerateConvertedIdentifiers(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name                string
		originalStoragePath string
		format              string
		wantIdentifier      string
		wantStoragePath     string
	}{
		{
			name:                "webp format",
			originalStoragePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			format:              "webp",
			wantIdentifier:      "a1b2c3d4e5f6",
			wantStoragePath:     "converted/webp/2024/01/15/a1b2c3d4e5f6.webp",
		},
		{
			name:                "avif format",
			originalStoragePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			format:              "avif",
			wantIdentifier:      "a1b2c3d4e5f6",
			wantStoragePath:     "converted/avif/2024/01/15/a1b2c3d4e5f6.avif",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.GenerateConvertedIdentifiers(tt.originalStoragePath, tt.format)
			if got.Identifier != tt.wantIdentifier {
				t.Errorf("GenerateConvertedIdentifiers() Identifier = %v, want %v", got.Identifier, tt.wantIdentifier)
			}
			if got.StoragePath != tt.wantStoragePath {
				t.Errorf("GenerateConvertedIdentifiers() StoragePath = %v, want %v", got.StoragePath, tt.wantStoragePath)
			}
		})
	}
}

func TestPathGenerator_extractHashFromPath(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name        string
		storagePath string
		want        string
	}{
		{
			name:        "simple identifier",
			storagePath: "a1b2c3d4e5f6.jpg",
			want:        "a1b2c3d4e5f6",
		},
		{
			name:        "hierarchical path",
			storagePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			want:        "a1b2c3d4e5f6",
		},
		{
			name:        "thumbnail path",
			storagePath: "thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp",
			want:        "a1b2c3d4e5f6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.extractHashFromPath(tt.storagePath)
			if got != tt.want {
				t.Errorf("extractHashFromPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_extractDatePath(t *testing.T) {
	pg := NewPathGenerator()

	tests := []struct {
		name        string
		storagePath string
		want        string
	}{
		{
			name:        "hierarchical path",
			storagePath: "original/2024/01/15/a1b2c3d4e5f6.jpg",
			want:        "2024/01/15",
		},
		{
			name:        "old format returns current date",
			storagePath: "a1b2c3d4e5f6.jpg",
			want:        time.Now().Format("2006/01/02"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pg.extractDatePath(tt.storagePath)
			if got != tt.want {
				t.Errorf("extractDatePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathGenerator_Consistency(t *testing.T) {
	pg := NewPathGenerator()
	uploadTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	fileHash := "a1b2c3d4e5f6abcdef123456"
	ext := ".jpg"

	orig := pg.GenerateOriginalIdentifiers(fileHash, ext, uploadTime)

	if !strings.HasPrefix(orig.StoragePath, "original/2024/01/15/") {
		t.Errorf("Original storage path has wrong date: %s", orig.StoragePath)
	}

	thumb := pg.GenerateThumbnailIdentifiers(orig.StoragePath, 300)

	if !strings.Contains(thumb.StoragePath, "2024/01/15") {
		t.Errorf("Thumbnail storage path missing date: %s", thumb.StoragePath)
	}

	// 验证缩略图 identifier 包含宽度
	if !strings.HasSuffix(thumb.Identifier, "_300") {
		t.Errorf("Thumbnail identifier missing width: %s", thumb.Identifier)
	}

	webp := pg.GenerateConvertedIdentifiers(orig.StoragePath, "webp")

	if !strings.Contains(webp.StoragePath, "2024/01/15") {
		t.Errorf("WebP storage path missing date: %s", webp.StoragePath)
	}
}
