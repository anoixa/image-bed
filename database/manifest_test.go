package database

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedPhysicalTables is an INDEPENDENT, hand-maintained list of every
// physical table the application persists, in foreign-key dependency order.
// It is intentionally NOT derived from DurableTables: when a new model is added
// to AutoMigrate but not to the manifest (or vice versa), this explicit list is
// what catches the omission. Update it deliberately when the schema changes.
var expectedPhysicalTables = []string{
	"users",
	"devices",
	"images",
	"image_variants",
	"albums",
	"album_images",
	"api_tokens",
	"system_configs",
	"user_identities",
	"user_totp_settings",
	"two_factor_challenges",
}

// expectedSchemaModelTypes is the INDEPENDENT expected set of GORM models that
// AutoMigrate must create (album_images is excluded: it is an implicit
// many-to-many join GORM builds from the Album/Image tags).
var expectedSchemaModelTypes = []string{
	"*models.User",
	"*models.Device",
	"*models.Image",
	"*models.ImageVariant",
	"*models.Album",
	"*models.ApiToken",
	"*models.SystemConfig",
	"*models.UserIdentity",
	"*models.UserTOTPSetting",
	"*models.TwoFactorChallenge",
}

func manifestNames() []string {
	names := make([]string, len(DurableTables))
	for i, s := range DurableTables {
		names[i] = s.Name
	}
	return names
}

// TestManifestCoversAllPhysicalTables guards against the table-list drift that
// caused F3/F5: the manifest must list exactly the expected physical tables,
// in the expected order.
func TestManifestCoversAllPhysicalTables(t *testing.T) {
	assert.Equal(t, expectedPhysicalTables, manifestNames(),
		"DurableTables must match the independently maintained physical-table list")
}

// TestMigrationModelsMatchExpected ensures AutoMigrate (which now derives from
// MigrationModels) creates exactly the expected set of models. Comparing
// against an explicit type list — not against DurableTables — is what makes
// this test able to detect an omission.
func TestMigrationModelsMatchExpected(t *testing.T) {
	got := make([]string, 0, len(DurableTables))
	for _, m := range MigrationModels() {
		got = append(got, fmt.Sprintf("%T", m))
	}
	assert.Equal(t, expectedSchemaModelTypes, got)
}

func TestManifestNamesUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, s := range DurableTables {
		assert.Falsef(t, seen[s.Name], "duplicate table name in manifest: %s", s.Name)
		seen[s.Name] = true
	}
}

// TestManifestConstructors validates that every spec exposes the constructors
// its flags require, and that they return non-nil values.
func TestManifestConstructors(t *testing.T) {
	for _, s := range DurableTables {
		t.Run(s.Name, func(t *testing.T) {
			if s.IsJoinTable {
				assert.Nil(t, s.SchemaModel, "join table must have a nil SchemaModel")
			} else {
				require.NotNil(t, s.SchemaModel, "non-join table must have a SchemaModel")
				assert.NotNil(t, s.SchemaModel(), "SchemaModel() must return a value")
			}
			if s.BackupData {
				require.NotNil(t, s.NewRecord, "BackupData table must have a record codec")
				require.NotNil(t, s.NewRecordSlice, "BackupData table must have a slice codec")
				assert.NotNil(t, s.NewRecord())
				assert.NotNil(t, s.NewRecordSlice())
			}
		})
	}
}

// TestManifestJoinTable asserts album_images is the only implicit join entry
// and has the right flags (composite key => no auto id; still backed up).
func TestManifestJoinTable(t *testing.T) {
	var joins []string
	for _, s := range DurableTables {
		if s.IsJoinTable {
			joins = append(joins, s.Name)
		}
	}
	require.Equal(t, []string{"album_images"}, joins, "album_images must be the only join table")

	spec, ok := TableByName("album_images")
	require.True(t, ok)
	assert.Nil(t, spec.SchemaModel)
	assert.False(t, spec.HasAutoID, "composite join key has no auto-increment id")
	assert.True(t, spec.BackupData)
	assert.True(t, spec.MigrateData)
}

// TestManifestEphemeralTable asserts two_factor_challenges is schema-only and
// is cleared on a full restore but never backed up or migrated.
func TestManifestEphemeralTable(t *testing.T) {
	spec, ok := TableByName("two_factor_challenges")
	require.True(t, ok)
	assert.NotNil(t, spec.SchemaModel, "ephemeral table still needs a schema")
	assert.False(t, spec.BackupData, "ephemeral table must not be backed up")
	assert.False(t, spec.MigrateData, "ephemeral table must not be migrated")
	assert.True(t, spec.ClearOnFullRestore, "ephemeral table must be cleared on full restore")
}

// TestManifestFlagConsistency checks cross-field invariants.
func TestManifestFlagConsistency(t *testing.T) {
	for _, s := range DurableTables {
		// Only the join table is a join table; all model tables have auto ids.
		if !s.IsJoinTable {
			assert.Truef(t, s.HasAutoID, "%s: model tables are expected to have an auto-increment id", s.Name)
		}
		// A schema-only table (no data) is the only legitimate ClearOnFullRestore user.
		if s.ClearOnFullRestore {
			assert.Falsef(t, s.BackupData, "%s: ClearOnFullRestore is for ephemeral (non-backed-up) tables", s.Name)
		}
	}
}

// TestManifestDependencyOrder verifies parents precede children so forward
// order is a safe insert order and the reverse is a safe delete order.
func TestManifestDependencyOrder(t *testing.T) {
	pos := make(map[string]int, len(DurableTables))
	for i, s := range DurableTables {
		pos[s.Name] = i
	}
	before := func(parent, child string) {
		t.Helper()
		assert.Lessf(t, pos[parent], pos[child], "%s must come before %s", parent, child)
	}
	for _, child := range []string{"devices", "images", "albums", "api_tokens", "user_identities", "user_totp_settings", "two_factor_challenges"} {
		before("users", child)
	}
	before("images", "image_variants")
	before("albums", "album_images")
	before("images", "album_images")
}

func TestTruncateOrderIsReverseAndConditional(t *testing.T) {
	all := manifestNames()

	// Full restore of all data tables: reverse FK order, plus the ephemeral
	// table cleared first (it is last in forward order).
	full := TruncateOrder(all, true)
	require.NotEmpty(t, full)
	assert.Equal(t, "two_factor_challenges", full[0], "ephemeral table cleared first on full restore")
	assert.Equal(t, "users", full[len(full)-1], "users (root parent) cleared last")

	// Without fullRestore the ephemeral table is not cleared.
	partial := TruncateOrder([]string{"users", "images"}, false)
	assert.Equal(t, []string{"images", "users"}, partial)
	assert.NotContains(t, TruncateOrder(all, false), "two_factor_challenges")
}

func TestRestoreOrderFiltersAndOrders(t *testing.T) {
	// Selection is restored in manifest order regardless of input order, and
	// excludes non-BackupData tables even if requested.
	got := RestoreOrder([]string{"images", "users", "two_factor_challenges"})
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = s.Name
	}
	assert.Equal(t, []string{"users", "images"}, names)
}

func TestAutoIDTablesExcludesJoin(t *testing.T) {
	got := AutoIDTables([]string{"users", "album_images", "images"})
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = s.Name
	}
	assert.Equal(t, []string{"users", "images"}, names, "album_images has no auto id")
}
