package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateManagerSignAndVerify(t *testing.T) {
	sm := NewStateManager([]byte("test-secret-key-at-least-32-bytes"))

	state := NewOAuthState(StateModeLogin, "github", "/dashboard")

	signed, err := sm.SignState(state)
	require.NoError(t, err)
	assert.NotEmpty(t, signed)

	verified, err := sm.VerifyState(signed)
	require.NoError(t, err)
	assert.Equal(t, StateModeLogin, verified.Mode)
	assert.Equal(t, "github", verified.Provider)
	assert.Equal(t, "/dashboard", verified.ReturnTo)
}

func TestStateManagerTamperDetection(t *testing.T) {
	sm := NewStateManager([]byte("test-secret-key-at-least-32-bytes"))

	state := NewOAuthState(StateModeLogin, "github", "/dashboard")
	signed, err := sm.SignState(state)
	require.NoError(t, err)

	// Tamper with the state
	tampered := signed + "x"
	_, err = sm.VerifyState(tampered)
	assert.Error(t, err, "tampered state should fail verification")
}

func TestStateManagerExpiry(t *testing.T) {
	sm := NewStateManager([]byte("test-secret-key-at-least-32-bytes"))

	state := NewOAuthState(StateModeLogin, "github", "/dashboard")
	// Manually expire the state
	state.ExpireAt = time.Now().Add(-1 * time.Hour).Unix()

	signed, err := sm.SignState(state)
	require.NoError(t, err)

	_, err = sm.VerifyState(signed)
	assert.Error(t, err, "expired state should fail verification")
}

func TestStateManagerInvalidFormat(t *testing.T) {
	sm := NewStateManager([]byte("test-secret-key-at-least-32-bytes"))

	_, err := sm.VerifyState("invalid-no-dot")
	assert.Error(t, err)
}

func TestStateManagerWrongKey(t *testing.T) {
	sm1 := NewStateManager([]byte("secret-key-number-one-32bytes!!"))
	sm2 := NewStateManager([]byte("secret-key-number-two-32bytes!!"))

	state := NewOAuthState(StateModeLogin, "github", "/dashboard")
	signed, err := sm1.SignState(state)
	require.NoError(t, err)

	_, err = sm2.VerifyState(signed)
	assert.Error(t, err, "state signed with different key should fail")
}

func TestStateManagerLinkModeWithUserID(t *testing.T) {
	sm := NewStateManager([]byte("test-secret-key-at-least-32-bytes"))

	state := NewOAuthState(StateModeLink, "google", "/settings")
	state.UserID = 42

	signed, err := sm.SignState(state)
	require.NoError(t, err)

	verified, err := sm.VerifyState(signed)
	require.NoError(t, err)
	assert.Equal(t, StateModeLink, verified.Mode)
	assert.Equal(t, uint(42), verified.UserID)
}

func TestValidateReturnTo(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"", true},
		{"/dashboard", true},
		{"/settings/account", true},
		{"//evil.com", false},
		{"https://evil.com", false},
		{"http://evil.com", false},
		{"/path/with?query=true", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.valid, ValidateReturnTo(tc.input))
		})
	}
}

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	require.NoError(t, err)
	assert.NotEmpty(t, verifier)
	assert.NotEmpty(t, challenge)
	assert.NotEqual(t, verifier, challenge)
}

func TestGenerateNonce(t *testing.T) {
	nonce1, err := GenerateNonce()
	require.NoError(t, err)
	nonce2, err := GenerateNonce()
	require.NoError(t, err)
	assert.NotEqual(t, nonce1, nonce2, "nonces should be unique")
}
