package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptStringFailsClosedWhenUninitialized(t *testing.T) {
	svc := &Service{}

	encrypted, err := svc.EncryptString(`{"secret":"value"}`)
	require.Error(t, err)
	assert.Empty(t, encrypted)
	assert.Contains(t, err.Error(), "encryptor not initialized")
}
