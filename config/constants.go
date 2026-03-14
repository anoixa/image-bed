package config

// HTTP Headers
const (
	ContentTypeJSON     = "application/json"
	ContentTypeJPEG     = "image/jpeg"
	ContentTypeWebP     = "image/webp"
	ContentTypePNG      = "image/png"
	ContentTypeGIF      = "image/gif"
	CacheControlPublic  = "public, max-age=86400"
	CacheControlPrivate = "private, max-age=3600"
	CacheControlNoStore = "no-store"
)

// Paths
const (
	TempDir           = "./data/temp"
	DefaultDataDir    = "./data"
	DefaultUploadDir  = "./uploads"
	DefaultStorageDir = "./storage"
)

// Pagination
const (
	DefaultPage     = 1
	DefaultPerPage  = 10
	MaxPerPage      = 100
	DefaultPageSize = 20
)

// File Size Limits
const (
	DefaultMaxUploadSize    = 50 * 1024 * 1024  // 50MB
	DefaultMaxImageSize     = 10 * 1024 * 1024  // 10MB
	DefaultMaxThumbnailSize = 5 * 1024 * 1024   // 5MB
)

// Timeouts
const (
	DefaultRequestTimeout   = 30 // seconds
	DefaultDBQueryTimeout   = 10 // seconds
	DefaultCacheExpiration  = 3600 // seconds (1 hour)
	DefaultJWTExpiration    = 24 * 3600 // seconds (24 hours)
)
