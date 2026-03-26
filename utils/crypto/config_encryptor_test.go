package cryptopackage

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecureKeyGetReturnsCopy(t *testing.T) {
	original := []byte("0123456789abcdef0123456789abcdef")
	sk := &SecureKey{key: append([]byte(nil), original...)}

	got := sk.Get()
	require.True(t, bytes.Equal(original, got))

	got[0] = 'x'
	assert.Equal(t, byte('0'), sk.key[0])
}

func TestConfigEncryptorEncryptFailsClosedWithoutKey(t *testing.T) {
	encryptor := NewConfigEncryptor(nil)

	encrypted, err := encryptor.Encrypt(`{"secret":"value"}`)
	require.Error(t, err)
	assert.Empty(t, encrypted)
	assert.Contains(t, err.Error(), "master key not available")
}

func TestConfigEncryptorEncryptProducesEncryptedPayload(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encryptor := NewConfigEncryptor(key)

	encrypted, err := encryptor.Encrypt(`{"secret":"value"}`)
	require.NoError(t, err)
	assert.NotEqual(t, `{"secret":"value"}`, encrypted)
	assert.True(t, IsEncrypted(encrypted))
}
