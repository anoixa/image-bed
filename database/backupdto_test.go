package database

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// parseColumns returns the set of physical column DB names for a GORM model or
// backup DTO. Association struct fields are relationships, not columns, so they
// are naturally excluded from Schema.DBNames.
func parseColumns(t *testing.T, model any) map[string]bool {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	require.NoErrorf(t, err, "failed to parse schema for %T", model)
	cols := make(map[string]bool, len(s.DBNames))
	for _, name := range s.DBNames {
		cols[name] = true
	}
	return cols
}

// TestV2RecordCoversEveryColumn is the drift guard for the v2 backup format.
//
// For every backup table it reflects over the authoritative GORM schema (the
// real model) and asserts the table's v2 record exposes a field for every
// physical column. This is what makes a future column that forgets its backup
// DTO a test failure rather than a silent, security-relevant data-loss bug on
// restore (the original F3: users.password / system_configs.config_json were
// dropped by `json:"-"` and produced unusable restored rows).
func TestV2RecordCoversEveryColumn(t *testing.T) {
	for _, spec := range BackupTables() {
		t.Run(spec.Name, func(t *testing.T) {
			// The authoritative column set comes from the schema model, except
			// for the implicit join table, which has no model — its record type
			// is the authority there.
			var modelCols map[string]bool
			if spec.SchemaModel != nil {
				modelCols = parseColumns(t, spec.SchemaModel())
			} else {
				modelCols = parseColumns(t, spec.NewRecord())
			}

			v2Cols := parseColumns(t, spec.V2Record())

			for col := range modelCols {
				require.Truef(t, v2Cols[col],
					"v2 backup record for %q is missing physical column %q; add it to the backup DTO in database/backupdto.go",
					spec.Name, col)
			}
		})
	}
}

// TestV2RecordExposesSensitiveColumns pins the specific columns whose `json:"-"`
// omission is the F3 bug, guarding against a regression that reintroduces the
// silent drop even if the column still technically exists on the DTO.
func TestV2RecordExposesSensitiveColumns(t *testing.T) {
	cases := map[string][]string{
		"users":          {"password"},
		"system_configs": {"config_json", "deleted_at"},
		"image_variants": {"deleted_at"},
		"images":         {"is_pending_deletion"},
	}
	for table, cols := range cases {
		spec, ok := TableByName(table)
		require.Truef(t, ok, "table %q must exist in the manifest", table)
		v2Cols := parseColumns(t, spec.V2Record())
		for _, col := range cols {
			require.Truef(t, v2Cols[col],
				"v2 backup record for %q must expose column %q (was hidden by json:\"-\")", table, col)
		}
	}
}
