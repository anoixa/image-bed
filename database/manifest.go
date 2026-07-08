package database

import (
	"github.com/anoixa/image-bed/database/models"
)

// AlbumImageRecord is the recovery record for the implicit album_images
// many-to-many join table. The join has no standalone GORM model, so backup,
// restore and migration operate on this composite-key record directly.
//
// The JSON field names match the legacy v1 archive serialization
// (see cmd/backup.go) so existing archives remain readable.
type AlbumImageRecord struct {
	AlbumID uint `json:"album_id"`
	ImageID uint `json:"image_id"`
}

// TableSpec declares one physical table for the disaster-recovery tooling
// (backup, restore, migrate) and for schema migration (AutoMigrate). It is the
// single authoritative description of a durable table; the maintenance commands
// must derive their behavior from DurableTables rather than keeping their own
// divergent table lists.
type TableSpec struct {
	// Name is the physical table name (e.g. "users", "album_images").
	Name string

	// SchemaModel returns a pointer to the GORM model used for AutoMigrate.
	// It is nil for implicit join tables (album_images), which GORM creates
	// via the many2many tags on their owning models.
	SchemaModel func() any

	// NewRecord / NewRecordSlice produce the recovery record type used to
	// decode and insert rows during restore, and to query rows during backup.
	//
	// The v1 record codec decodes legacy v1 archives, whose JSONL was produced
	// from the GORM models, so NewRecord/NewRecordSlice return the models
	// themselves.
	NewRecord      func() any
	NewRecordSlice func() any

	// The v2 record codec is the dedicated backup-DTO type used by the v2
	// archive format. The GORM models drop security-sensitive and internal
	// columns from their JSON via `json:"-"` (users.password,
	// system_configs.config_json, soft-delete state, images.is_pending_deletion);
	// the v2 DTO re-exposes every physical column so a backup round-trips
	// losslessly. When nil, the table's model already serializes all columns and
	// the v1 codec is reused — see V2Record/V2RecordSlice.
	NewV2Record      func() any
	NewV2RecordSlice func() any

	// HasAutoID is true when the table has an auto-increment primary key whose
	// sequence must be reset after a restore or migration.
	HasAutoID bool

	// BackupData marks tables whose rows belong in a backup/restore archive.
	// Ephemeral tables (two_factor_challenges) are schema-only: false here.
	BackupData bool

	// MigrateData marks tables whose rows are copied during cross-database
	// migration.
	MigrateData bool

	// ClearOnFullRestore marks tables that must be cleared during a default
	// full (--truncate) restore even though they are not restored from data.
	// Used for ephemeral tables so stale challenges do not survive a restore.
	ClearOnFullRestore bool

	// IsJoinTable marks the implicit many-to-many join table (album_images),
	// which has a composite key, no SchemaModel, and requires exact inserts.
	IsJoinTable bool
}

// DurableTables is the authoritative, foreign-key-ordered list of every
// physical table the application persists. Forward order is a safe
// insert/backup order (parents before children); the reverse is a safe
// delete/truncate order (children before parents).
//
// IMPORTANT: when a new persistent model is added to AutoMigrate, it must also
// be added here, and database/manifest_test.go enforces that the two lists stay
// in sync. This is the single source of truth that prevents the historical
// backup/restore/migrate table-list drift.
var DurableTables = []TableSpec{
	{
		Name:             "users",
		SchemaModel:      func() any { return &models.User{} },
		NewRecord:        func() any { return &models.User{} },
		NewRecordSlice:   func() any { return &[]models.User{} },
		NewV2Record:      func() any { return &BackupUser{} },
		NewV2RecordSlice: func() any { return &[]BackupUser{} },
		HasAutoID:        true,
		BackupData:       true,
		MigrateData:      true,
	},
	{
		Name:           "devices",
		SchemaModel:    func() any { return &models.Device{} },
		NewRecord:      func() any { return &models.Device{} },
		NewRecordSlice: func() any { return &[]models.Device{} },
		HasAutoID:      true,
		BackupData:     true,
		MigrateData:    true,
	},
	{
		Name:             "images",
		SchemaModel:      func() any { return &models.Image{} },
		NewRecord:        func() any { return &models.Image{} },
		NewRecordSlice:   func() any { return &[]models.Image{} },
		NewV2Record:      func() any { return &BackupImage{} },
		NewV2RecordSlice: func() any { return &[]BackupImage{} },
		HasAutoID:        true,
		BackupData:       true,
		MigrateData:      true,
	},
	{
		Name:             "image_variants",
		SchemaModel:      func() any { return &models.ImageVariant{} },
		NewRecord:        func() any { return &models.ImageVariant{} },
		NewRecordSlice:   func() any { return &[]models.ImageVariant{} },
		NewV2Record:      func() any { return &BackupImageVariant{} },
		NewV2RecordSlice: func() any { return &[]BackupImageVariant{} },
		HasAutoID:        true,
		BackupData:       true,
		MigrateData:      true,
	},
	{
		Name:           "albums",
		SchemaModel:    func() any { return &models.Album{} },
		NewRecord:      func() any { return &models.Album{} },
		NewRecordSlice: func() any { return &[]models.Album{} },
		HasAutoID:      true,
		BackupData:     true,
		MigrateData:    true,
	},
	{
		Name:           "album_images",
		SchemaModel:    nil, // implicit many2many join, created via Album/Image tags
		NewRecord:      func() any { return &AlbumImageRecord{} },
		NewRecordSlice: func() any { return &[]AlbumImageRecord{} },
		HasAutoID:      false,
		BackupData:     true,
		MigrateData:    true,
		IsJoinTable:    true,
	},
	{
		Name:           "api_tokens",
		SchemaModel:    func() any { return &models.ApiToken{} },
		NewRecord:      func() any { return &models.ApiToken{} },
		NewRecordSlice: func() any { return &[]models.ApiToken{} },
		HasAutoID:      true,
		BackupData:     true,
		MigrateData:    true,
	},
	{
		Name:             "system_configs",
		SchemaModel:      func() any { return &models.SystemConfig{} },
		NewRecord:        func() any { return &models.SystemConfig{} },
		NewRecordSlice:   func() any { return &[]models.SystemConfig{} },
		NewV2Record:      func() any { return &BackupSystemConfig{} },
		NewV2RecordSlice: func() any { return &[]BackupSystemConfig{} },
		HasAutoID:        true,
		BackupData:       true,
		MigrateData:      true,
	},
	{
		Name:           "user_identities",
		SchemaModel:    func() any { return &models.UserIdentity{} },
		NewRecord:      func() any { return &models.UserIdentity{} },
		NewRecordSlice: func() any { return &[]models.UserIdentity{} },
		HasAutoID:      true,
		BackupData:     true,
		MigrateData:    true,
	},
	{
		Name:           "user_totp_settings",
		SchemaModel:    func() any { return &models.UserTOTPSetting{} },
		NewRecord:      func() any { return &models.UserTOTPSetting{} },
		NewRecordSlice: func() any { return &[]models.UserTOTPSetting{} },
		HasAutoID:      true,
		BackupData:     true,
		MigrateData:    true,
	},
	{
		Name:               "two_factor_challenges",
		SchemaModel:        func() any { return &models.TwoFactorChallenge{} },
		HasAutoID:          true,
		BackupData:         false, // ephemeral single-use login challenges
		MigrateData:        false,
		ClearOnFullRestore: true,
	},
}

// MigrationModels returns the GORM models for AutoMigrate, in manifest order,
// skipping implicit join tables (which GORM creates from many2many tags).
func MigrationModels() []any {
	out := make([]any, 0, len(DurableTables))
	for _, spec := range DurableTables {
		if spec.SchemaModel == nil {
			continue
		}
		out = append(out, spec.SchemaModel())
	}
	return out
}

// BackupTables returns the specs whose rows are included in a backup archive,
// in manifest (insert) order.
func BackupTables() []TableSpec {
	return filterSpecs(func(s TableSpec) bool { return s.BackupData })
}

// MigrateTables returns the specs whose rows are copied during cross-database
// migration, in manifest order.
func MigrateTables() []TableSpec {
	return filterSpecs(func(s TableSpec) bool { return s.MigrateData })
}

// AllTables returns all table specs in manifest order.
func AllTables() []TableSpec {
	return DurableTables
}

// RestoreOrder returns the BackupData specs to restore, in manifest (insert)
// order, limited to the selected table names.
func RestoreOrder(selected []string) []TableSpec {
	want := nameSet(selected)
	return filterSpecs(func(s TableSpec) bool { return s.BackupData && want[s.Name] })
}

// TruncateOrder returns the table names to clear before a --truncate restore,
// in reverse manifest (delete) order. Selected durable data tables are always
// included; when fullRestore is true, ClearOnFullRestore tables (ephemeral
// tables such as two_factor_challenges) are also cleared.
func TruncateOrder(selected []string, fullRestore bool) []string {
	want := nameSet(selected)
	var names []string
	for i := len(DurableTables) - 1; i >= 0; i-- {
		s := DurableTables[i]
		switch {
		case s.BackupData && want[s.Name]:
			names = append(names, s.Name)
		case fullRestore && s.ClearOnFullRestore:
			names = append(names, s.Name)
		}
	}
	return names
}

// AutoIDTables returns the selected specs that have an auto-increment primary
// key whose sequence must be reset, in manifest order.
func AutoIDTables(selected []string) []TableSpec {
	want := nameSet(selected)
	return filterSpecs(func(s TableSpec) bool { return s.HasAutoID && want[s.Name] })
}

// TableByName looks up a spec by its exact physical table name.
func TableByName(name string) (TableSpec, bool) {
	for _, s := range DurableTables {
		if s.Name == name {
			return s, true
		}
	}
	return TableSpec{}, false
}

func filterSpecs(keep func(TableSpec) bool) []TableSpec {
	out := make([]TableSpec, 0, len(DurableTables))
	for _, s := range DurableTables {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// V2Record returns a pointer to the table's v2 backup record (the dedicated
// backup DTO), falling back to the v1 model record when the table has no DTO
// because its model already serializes every column.
func (s TableSpec) V2Record() any {
	if s.NewV2Record != nil {
		return s.NewV2Record()
	}
	return s.NewRecord()
}

// V2RecordSlice returns a pointer to a slice of the table's v2 backup record,
// falling back to the v1 model slice when the table has no DTO.
func (s TableSpec) V2RecordSlice() any {
	if s.NewV2RecordSlice != nil {
		return s.NewV2RecordSlice()
	}
	return s.NewRecordSlice()
}
