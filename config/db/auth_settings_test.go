package config

import (
	"context"
	"errors"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/configs"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// errAuthRepo is a configs.Repository whose GetByKey returns a fixed error.
// All other methods are inherited from the embedded nil interface and must not
// be reached by the tests below.
type errAuthRepo struct {
	configs.Repository
	err error
}

func (r *errAuthRepo) GetByKey(_ context.Context, _ string) (*models.SystemConfig, error) {
	return nil, r.err
}

// IsPasswordLoginEnabled must fail closed: an operational error loading the
// dynamic security config must NOT re-enable password login (F2 fail-open fix).
func TestIsPasswordLoginEnabled_FailsClosedOnRepoError(t *testing.T) {
	m := &Manager{
		repo:  &errAuthRepo{err: errors.New("database unavailable")},
		cache: NewCacheLayer(),
	}

	// Even with the static fallback set to true, an operational error must
	// disable password login rather than silently re-enabling it.
	got := m.IsPasswordLoginEnabled(context.Background(), true)
	assert.False(t, got, "operational config error must fail closed (disable password login)")
}

// parseAuthSettings must strictly require password_login_enabled to be a
// present boolean. A successfully decrypted but malformed payload ({}, null or
// a wrong-typed value) must produce an error so the caller fails closed,
// rather than silently coercing to the static default (which re-opens login).
func TestParseAuthSettings_Strict(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]any
		wantErr   bool
		want      bool
	}{
		{name: "valid_true", configMap: map[string]any{"password_login_enabled": true}, want: true},
		{name: "valid_false", configMap: map[string]any{"password_login_enabled": false}, want: false},
		{name: "missing_key", configMap: map[string]any{}, wantErr: true},
		{name: "nil_value", configMap: map[string]any{"password_login_enabled": nil}, wantErr: true},
		{name: "string_true", configMap: map[string]any{"password_login_enabled": "true"}, wantErr: true},
		{name: "number", configMap: map[string]any{"password_login_enabled": float64(1)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings, err := parseAuthSettings(tt.configMap)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, settings)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, settings.PasswordLoginEnabled)
		})
	}
}

// A missing record (never configured) is not a failure; the static default
// must still apply so first-run deployments keep password login available.
func TestIsPasswordLoginEnabled_RecordNotFoundUsesFallback(t *testing.T) {
	m := &Manager{
		repo:  &errAuthRepo{err: gorm.ErrRecordNotFound},
		cache: NewCacheLayer(),
	}

	assert.True(t, m.IsPasswordLoginEnabled(context.Background(), true),
		"record-not-found must use the static default (true)")

	// And honour an explicit static default of false.
	m.cache = NewCacheLayer() // reset cached settings
	assert.False(t, m.IsPasswordLoginEnabled(context.Background(), false),
		"record-not-found must honour a static default of false")
}
