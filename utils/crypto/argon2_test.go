package cryptopackage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateFromPassword_Success 测试密码哈希生成成功
func TestGenerateFromPassword_Success(t *testing.T) {
	password := "mysecretpassword123"

	hash, err := GenerateFromPassword(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.True(t, strings.HasPrefix(hash, "$argon2id$"))
	assert.Contains(t, hash, "$v=")
	assert.Contains(t, hash, "$m=")
	assert.Contains(t, hash, ",t=")
	assert.Contains(t, hash, ",p=")
}

// TestGenerateFromPassword_DifferentHashes 测试相同密码产生不同哈希
func TestGenerateFromPassword_DifferentHashes(t *testing.T) {
	password := "samepassword123"

	hash1, err := GenerateFromPassword(password)
	require.NoError(t, err)

	hash2, err := GenerateFromPassword(password)
	require.NoError(t, err)

	// 相同密码应该产生不同哈希（盐值不同）
	assert.NotEqual(t, hash1, hash2)
}

// TestComparePasswordAndHash_Success 测试密码验证成功
func TestComparePasswordAndHash_Success(t *testing.T) {
	password := "correctpassword123"

	hash, err := GenerateFromPassword(password)
	require.NoError(t, err)

	match, err := ComparePasswordAndHash(password, hash)
	require.NoError(t, err)
	assert.True(t, match)
}

// TestComparePasswordAndHash_WrongPassword 测试错误密码
func TestComparePasswordAndHash_WrongPassword(t *testing.T) {
	password := "correctpassword123"
	wrongPassword := "wrongpassword123"

	hash, err := GenerateFromPassword(password)
	require.NoError(t, err)

	match, err := ComparePasswordAndHash(wrongPassword, hash)
	require.NoError(t, err)
	assert.False(t, match)
}

// TestComparePasswordAndHash_InvalidFormat 测试无效哈希格式
func TestComparePasswordAndHash_InvalidFormat(t *testing.T) {
	invalidHashes := []string{
		"",
		"invalid",
		"$argon2i$v=19$m=65536,t=2,p=4$salt$hash", // wrong algorithm
		"$argon2id$v=19$m=65536,t=2,p=4$",         // missing parts
		"$argon2id$v=19$m=65536,t=2,p=4$salt",     // missing hash
	}

	for _, hash := range invalidHashes {
		match, err := ComparePasswordAndHash("password", hash)
		assert.Error(t, err, "hash: %s", hash)
		assert.False(t, match, "hash: %s", hash)
	}
}

// TestComparePasswordAndHash_InvalidVersion 测试无效版本
func TestComparePasswordAndHash_InvalidVersion(t *testing.T) {

	hash := "$argon2id$vx=19$m=65536,t=2,p=4$c2FsdA$hash"
	match, err := ComparePasswordAndHash("password", hash)
	assert.Error(t, err)
	assert.False(t, match)
}

// TestComparePasswordAndHash_InvalidCostParams 测试无效成本参数
func TestComparePasswordAndHash_InvalidCostParams(t *testing.T) {

	hash := "$argon2id$v=19$invalid_params$c2FsdA$hash"
	match, err := ComparePasswordAndHash("password", hash)
	assert.Error(t, err)
	assert.False(t, match)
}

// TestComparePasswordAndHash_InvalidBase64 测试无效Base64
func TestComparePasswordAndHash_InvalidBase64(t *testing.T) {
	// 有效的格式但无效的base64
	hash := "$argon2id$v=19$m=65536,t=2,p=4$!!!invalid!!!$!!!invalid!!!"
	match, err := ComparePasswordAndHash("password", hash)
	assert.Error(t, err)
	assert.False(t, match)
}

// TestPasswordHashRoundTrip 测试完整流程
func TestPasswordHashRoundTrip(t *testing.T) {
	passwords := []string{
		"short",
		"medium length password",
		"a very long password with many characters and symbols !@#$%^&*()",
		"密码测试", // Unicode
		"🔐🔑🔒",  // Emoji
	}

	for _, password := range passwords {
		hash, err := GenerateFromPassword(password)
		require.NoError(t, err, "password: %s", password)

		match, err := ComparePasswordAndHash(password, hash)
		require.NoError(t, err, "password: %s", password)
		assert.True(t, match, "password: %s", password)

		match, err = ComparePasswordAndHash(password+"wrong", hash)
		require.NoError(t, err, "password: %s", password)
		assert.False(t, match, "password: %s", password)
	}
}

// TestArgon2Parameters 测试Argon2参数常量
func TestArgon2Parameters(t *testing.T) {
	assert.Equal(t, uint32(65536), argon2Memory)  // 64 MB
	assert.Equal(t, uint32(3), argon2Iterations)  // 2 iterations
	assert.Equal(t, uint8(4), argon2Parallelism)  // 4 threads
	assert.Equal(t, uint32(16), argon2SaltLength) // 16 bytes
	assert.Equal(t, uint32(32), argon2KeyLength)  // 32 bytes

	assert.GreaterOrEqual(t, argon2Memory, uint32(65536), "memory should be at least 64MB")
	assert.GreaterOrEqual(t, argon2SaltLength, uint32(16), "salt length should be at least 16 bytes")
	assert.GreaterOrEqual(t, argon2KeyLength, uint32(32), "key length should be at least 32 bytes")
}

// BenchmarkGenerateFromPassword 基准测试密码哈希生成
func BenchmarkGenerateFromPassword(b *testing.B) {
	password := "benchmarkpassword123"
	for i := 0; i < b.N; i++ {
		_, err := GenerateFromPassword(password)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkComparePasswordAndHash 基准测试密码验证
func BenchmarkComparePasswordAndHash(b *testing.B) {
	password := "benchmarkpassword123"
	hash, err := GenerateFromPassword(password)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ComparePasswordAndHash(password, hash)
		if err != nil {
			b.Fatal(err)
		}
	}
}
