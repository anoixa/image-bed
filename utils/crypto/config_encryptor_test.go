package cryptopackage

import (
	"bytes"
	"encoding/base64"
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
	assert.Contains(t, err.Error(), "encryption key not available")
}

func TestConfigEncryptorEncryptProducesEncryptedPayload(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encryptor := NewConfigEncryptor(key)

	encrypted, err := encryptor.Encrypt(`{"secret":"value"}`)
	require.NoError(t, err)
	assert.NotEqual(t, `{"secret":"value"}`, encrypted)
	assert.True(t, IsEncrypted(encrypted))
}

func TestMasterKeyManagerDerivesConfigEncryptionKeyForFileKey(t *testing.T) {
	manager := NewMasterKeyManager(t.TempDir())
	require.NoError(t, manager.Initialize(nil))

	masterKey := manager.GetKey()
	encryptionKey, err := manager.GetConfigEncryptionKey()
	require.NoError(t, err)

	assert.NotEqual(t, masterKey, encryptionKey)
	assert.Len(t, encryptionKey, 32)
	assert.Equal(t, [][]byte{masterKey}, manager.GetLegacyConfigEncryptionKeys())
}

func TestMasterKeyManagerUsesEnvKeyDirectlyForConfigEncryption(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Setenv("CONFIG_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))

	manager := NewMasterKeyManager(t.TempDir())
	require.NoError(t, manager.Initialize(nil))

	encryptionKey, err := manager.GetConfigEncryptionKey()
	require.NoError(t, err)

	assert.Equal(t, key, encryptionKey)
	assert.Empty(t, manager.GetLegacyConfigEncryptionKeys())
}

func TestConfigEncryptorFallbackDecryptsLegacyMasterKeyCiphertext(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	derivedKey, err := deriveConfigEncryptionKey(masterKey)
	require.NoError(t, err)

	legacyEncryptor := NewConfigEncryptor(masterKey)
	legacyCiphertext, err := legacyEncryptor.Encrypt(`{"secret":"value"}`)
	require.NoError(t, err)

	newEncryptor := NewConfigEncryptorWithFallback(derivedKey, masterKey)
	plaintext, err := newEncryptor.Decrypt(legacyCiphertext)
	require.NoError(t, err)

	assert.Equal(t, `{"secret":"value"}`, plaintext)
}

func TestConfigEncryptorEncryptsWithDerivedPrimaryKey(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	derivedKey, err := deriveConfigEncryptionKey(masterKey)
	require.NoError(t, err)

	encryptor := NewConfigEncryptorWithFallback(derivedKey, masterKey)
	ciphertext, err := encryptor.Encrypt(`{"secret":"value"}`)
	require.NoError(t, err)

	_, err = NewConfigEncryptor(masterKey).Decrypt(ciphertext)
	require.Error(t, err)

	plaintext, err := NewConfigEncryptor(derivedKey).Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, `{"secret":"value"}`, plaintext)
}
