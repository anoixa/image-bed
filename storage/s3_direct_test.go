package storage

import (
	"testing"
)

func TestS3Storage_GetDirectURL(t *testing.T) {
	tests := []struct {
		name        string
		storage     *S3Storage
		storagePath string
		expectedURL string
	}{
		{
			name: "public domain with custom domain",
			storage: &S3Storage{
				bucketName:     "images",
				publicDomain:   "https://img.cdn.com",
				isPrivate:      false,
				forcePathStyle: true,
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "https://img.cdn.com/images/2024/01/test.jpg",
		},
		{
			name: "private bucket returns empty",
			storage: &S3Storage{
				bucketName:     "images",
				publicDomain:   "https://img.cdn.com",
				isPrivate:      true,
				forcePathStyle: true,
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "",
		},
		{
			name: "virtual host style without public domain",
			storage: &S3Storage{
				bucketName:     "images",
				endpoint:       "https://s3.amazonaws.com",
				isPrivate:      false,
				forcePathStyle: false,
				publicDomain:   "https://images.s3.amazonaws.com",
			},
			storagePath: "2024/01/test.jpg",
			expectedURL: "https://images.s3.amazonaws.com/2024/01/test.jpg",
		},
		{
			name: "path with special characters is encoded",
			storage: &S3Storage{
				bucketName:     "images",
				publicDomain:   "https://img.cdn.com",
				isPrivate:      false,
				forcePathStyle: true,
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

func TestS3Storage_SupportsDirectLink(t *testing.T) {
	tests := []struct {
		name     string
		storage  *S3Storage
		expected bool
	}{
		{
			name: "public bucket - supports direct link",
			storage: &S3Storage{
				isPrivate: false,
			},
			expected: true,
		},
		{
			name: "private bucket - not supported",
			storage: &S3Storage{
				isPrivate: true,
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

func TestS3Storage_ShouldProxy(t *testing.T) {
	tests := []struct {
		name          string
		storage       *S3Storage
		imageIsPublic bool
		globalMode    TransferMode
		expected      bool
	}{
		// Auto mode tests
		{
			name: "auto + public image + supports direct = no proxy",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAuto,
			expected:      false,
		},
		{
			name: "auto + private image = proxy",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: false,
			globalMode:    TransferModeAuto,
			expected:      true,
		},
		{
			name: "auto + public image + private bucket = proxy",
			storage: &S3Storage{
				isPrivate: true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAuto,
			expected:      true,
		},
		// Always proxy tests
		{
			name: "always_proxy + public image = proxy",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysProxy,
			expected:      true,
		},
		{
			name: "always_proxy + supports direct but global says proxy",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysProxy,
			expected:      true,
		},
		// Always direct tests
		{
			name: "always_direct + public image + supports = no proxy",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysDirect,
			expected:      false,
		},
		{
			name: "always_direct + public image + no support = proxy (fallback)",
			storage: &S3Storage{
				isPrivate: true,
			},
			imageIsPublic: true,
			globalMode:    TransferModeAlwaysDirect,
			expected:      true,
		},
		// Unknown mode fallback
		{
			name: "unknown mode = proxy (safe fallback)",
			storage: &S3Storage{
				isPrivate: false,
			},
			imageIsPublic: true,
			globalMode:    "unknown_mode",
			expected:      true,
		},
		// Empty global mode (should default to auto behavior)
		{
			name: "empty global mode + public image = no proxy",
			storage: &S3Storage{
				isPrivate: false,
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
