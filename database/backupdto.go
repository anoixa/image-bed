package database

import (
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

// Backup DTOs (the v2 archive record types).
//
// The GORM models drop security-sensitive and internal columns from their JSON
// representation via `json:"-"` (users.password, system_configs.config_json,
// system_configs.deleted_at, image_variants.deleted_at, images.is_pending_deletion).
// GORM itself ignores json tags, so a Find() populates those columns fine — the
// loss happens only at json.Encode/Decode. A v2 backup therefore round-trips
// through these dedicated DTOs, which mirror every physical column with explicit
// json tags so nothing is silently dropped, while keeping a TableName() so GORM
// still scans from and inserts into the real table.
//
// Tables whose models already serialize every physical column (devices, albums,
// album_images, api_tokens, user_identities, user_totp_settings) reuse their
// model as the v2 record type. database/manifest_test.go reflects over the GORM
// schema of every backup table and asserts its v2 record covers every column, so
// a future column that forgets a DTO update fails the test instead of silently
// corrupting a restore.

// BackupUser mirrors models.User but exposes the password hash, which the model
// hides with `json:"-"`.
type BackupUser struct {
	gorm.Model
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

// TableName targets the real users table.
func (BackupUser) TableName() string { return "users" }

// BackupSystemConfig mirrors models.SystemConfig but exposes the encrypted
// ConfigJSON blob and the soft-delete state, both hidden by `json:"-"`.
type BackupSystemConfig struct {
	ID        uint           `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at"`

	Category  models.ConfigCategory `json:"category"`
	Name      string                `json:"name"`
	Key       string                `json:"key"`
	IsEnabled bool                  `json:"is_enabled"`
	IsDefault bool                  `json:"is_default"`
	Priority  int                   `json:"priority"`

	ConfigJSON string `json:"config_json"`

	Description string `json:"description"`
	CreatedBy   uint   `json:"created_by"`
}

// TableName targets the real system_configs table.
func (BackupSystemConfig) TableName() string { return "system_configs" }

// BackupImageVariant mirrors models.ImageVariant but exposes the soft-delete
// state hidden by `json:"-"`.
type BackupImageVariant struct {
	ID        uint           `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at"`

	ImageID      uint       `json:"image_id"`
	Format       string     `json:"format"`
	Identifier   string     `json:"identifier"`
	StoragePath  string     `json:"storage_path"`
	FileSize     int64      `json:"file_size"`
	FileHash     string     `json:"file_hash"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	RetryCount   int        `json:"retry_count"`
	NextRetryAt  *time.Time `json:"next_retry_at,omitempty"`
}

// TableName targets the real image_variants table.
func (BackupImageVariant) TableName() string { return "image_variants" }

// BackupImage mirrors models.Image but exposes the internal IsPendingDeletion
// flag hidden by `json:"-"`. Associations (User, Albums) are intentionally
// omitted: they are not columns and restore never cascades into associations.
type BackupImage struct {
	ID        uint           `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at"`

	Identifier      string `json:"identifier"`
	StoragePath     string `json:"storage_path"`
	OriginalName    string `json:"original_name"`
	FileSize        int64  `json:"file_size"`
	MimeType        string `json:"mime_type"`
	StorageConfigID uint   `gorm:"column:storage_config_id" json:"storage_config_id"`

	FileHash string `json:"file_hash"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	IsPublic bool   `json:"is_public"`

	VariantStatus models.ImageVariantStatus `json:"variant_status"`

	UserID uint `json:"user_id"`

	IsPendingDeletion bool `json:"is_pending_deletion"`
}

// TableName targets the real images table.
func (BackupImage) TableName() string { return "images" }
