package storage

import (
	"testing"
)

func TestMinioStorage_GetDirectURL(t *testing.T) {
	tests := []struct {
		name        string
		storage     *MinioStorage
		storagePath string
		expectedURL string
	}{
		{
			name: "public endpoint with custom domain",
			storage: &MinioStorage{
				bucketName:       "images",
				publicEndpoint:   "https://img.cdn.com",
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "https://img.cdn.com/images/2024/01/test.jpg",
		},
		{
			name: "disabled direct link returns empty",
			storage: &MinioStorage{
				bucketName:       "images",
				publicEndpoint:   "https://img.cdn.com",
				enableDirectLink: false,
				isPublicBucket:   true,
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "",
		},
		{
			name: "non-public bucket returns empty",
			storage: &MinioStorage{
				bucketName:       "images",
				publicEndpoint:   "https://img.cdn.com",
				enableDirectLink: true,
				isPublicBucket:   false,
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "",
		},
		{
			name: "path with special characters is encoded",
			storage: &MinioStorage{
				bucketName:       "images",
				publicEndpoint:   "https://img.cdn.com",
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			storagePath: "2024/01/测试 图片.jpg",
			expectedURL: "https://img.cdn.com/images/2024/01/%E6%B5%8B%E8%AF%95%20%E5%9B%BE%E7%89%87.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.storage.GetDirectURL(tt.storagePath)
			if got != tt.expectedURL {
				t.Errorf("GetDirectURL() = %v, want %v", got, tt.expectedURL)
			}
		})
	}
}

func TestMinioStorage_SupportsDirectLink(t *testing.T) {
	tests := []struct {
		name     string
		storage  *MinioStorage
		expected bool
	}{
		{
			name: "all conditions met - supports direct link",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			expected: true,
		},
		{
			name: "direct link disabled - not supported",
			storage: &MinioStorage{
				enableDirectLink: false,
				isPublicBucket:   true,
			},
			expected: false,
		},
		{
			name: "not public bucket - not supported",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   false,
			},
			expected: false,
		},
		{
			name: "both disabled - not supported",
			storage: &MinioStorage{
				enableDirectLink: false,
				isPublicBucket:   false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.storage.SupportsDirectLink()
			if got != tt.expected {
				t.Errorf("SupportsDirectLink() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMinioStorage_ShouldProxy(t *testing.T) {
	tests := []struct {
		name          string
		storage       *MinioStorage
		imageIsPublic bool
		globalMode    TransferMode
		expected      bool
	}{
		// Auto mode tests
		{
			name: "auto + public image + supports direct = no proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAuto,
			expected:      false,
		},
		{
			name: "auto + private image = proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: false,
			globalMode:    TransferModeAuto,
			expected:      true,
		},
		{
			name: "auto + public image + no direct support = proxy",
			storage: &MinioStorage{
				enableDirectLink: false,
				isPublicBucket:   false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAuto,
			expected:      true,
		},
		// Always proxy tests
		{
			name: "always_proxy + public image = proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysProxy,
			expected:      true,
		},
		{
			name: "always_proxy + supports direct but global says proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysProxy,
			expected:      true,
		},
		// Always direct tests
		{
			name: "always_direct + public image + supports = no proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysDirect,
			expected:      false,
		},
		{
			name: "always_direct + public image + no support = proxy (fallback)",
			storage: &MinioStorage{
				enableDirectLink: false,
				isPublicBucket:   false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysDirect,
			expected:      true,
		},
		// Unknown mode fallback
		{
			name: "unknown mode = proxy (safe fallback)",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    "unknown_mode",
			expected:      true,
		},
		// Empty global mode (should default to auto behavior)
		{
			name: "empty global mode + public image = no proxy",
			storage: &MinioStorage{
				enableDirectLink: true,
				isPublicBucket:   true,
			},
			imageIsPublic: true,
			globalMode:    "",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.storage.ShouldProxy(tt.imageIsPublic, tt.globalMode)
			if got != tt.expected {
				t.Errorf("ShouldProxy() = %v, want %v", got, tt.expected)
			}
		})
	}
}
