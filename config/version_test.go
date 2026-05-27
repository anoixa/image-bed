package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimeEnvironmentOverridesBuildMetadata(t *testing.T) {
	oldVersion := Version
	oldCommitHash := CommitHash
	t.Cleanup(func() {
		Version = oldVersion
		CommitHash = oldCommitHash
	})

	Version = "dev"
	CommitHash = ""
	t.Setenv("APP_ENV", "production")

	assert.True(t, IsProduction())
	assert.False(t, IsDevelopment())
}
