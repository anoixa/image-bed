package utils

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateRandomToken_Success 测试随机Token生成
func TestGenerateRandomToken_Success(t *testing.T) {
	token, err := GenerateRandomToken(32)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

// TestGenerateRandomToken_Length 测试Token长度
func TestGenerateRandomToken_Length(t *testing.T) {
	tests := []struct {
		inputLength int
		minLength   int // base64编码后的最小长度
	}{
		{16, 22}, // 16字节 -> base64 URL编码后约22字符
		{32, 43}, // 32字节
		{64, 86}, // 64字节
		{128, 171},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			token, err := GenerateRandomToken(tt.inputLength)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(token), tt.minLength,
				"Token length should be at least %d for input %d", tt.minLength, tt.inputLength)
		})
	}
}

// TestGenerateRandomToken_Uniqueness 测试Token唯一性
func TestGenerateRandomToken_Uniqueness(t *testing.T) {
	const numTokens = 100
	tokens := make(map[string]bool)

	for i := 0; i < numTokens; i++ {
		token, err := GenerateRandomToken(32)
		require.NoError(t, err)

		// 检查是否重复
		if tokens[token] {
			t.Errorf("Duplicate token generated: %s", token)
		}
		tokens[token] = true
	}

	assert.Equal(t, numTokens, len(tokens), "All tokens should be unique")
}

// TestGenerateRandomToken_Base64Format 测试Base64编码格式
func TestGenerateRandomToken_Base64Format(t *testing.T) {
	token, err := GenerateRandomToken(64)
	require.NoError(t, err)

	// Base64 编码可能包含填充字符 =
	// 验证基本字符集（Base64标准字符 + 可能的填充）
	assert.Regexp(t, "^[A-Za-z0-9+/=_-]*$", token, "Token should be valid base64")

	// Token 应该非空
	assert.NotEmpty(t, token)
}

// TestGenerateRandomToken_EmptyLength 测试空长度
func TestGenerateRandomToken_EmptyLength(t *testing.T) {
	token, err := GenerateRandomToken(0)
	require.NoError(t, err)
	// 0字节base64编码后应该是空字符串
	assert.Empty(t, token)
}

// TestGenerateRandomToken_Concurrent 测试并发安全
func TestGenerateRandomToken_Concurrent(t *testing.T) {
	const numGoroutines = 50
	const tokensPerGoroutine = 20

	var wg sync.WaitGroup
	tokens := make(chan string, numGoroutines*tokensPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tokensPerGoroutine; j++ {
				token, err := GenerateRandomToken(32)
				if err != nil {
					t.Errorf("Failed to generate token: %v", err)
					return
				}
				tokens <- token
			}
		}()
	}

	wg.Wait()
	close(tokens)

	// 检查所有Token唯一性
	tokenMap := make(map[string]bool)
	for token := range tokens {
		if tokenMap[token] {
			t.Errorf("Duplicate token in concurrent generation: %s", token)
		}
		tokenMap[token] = true
	}

	assert.Equal(t, numGoroutines*tokensPerGoroutine, len(tokenMap))
}

// TestGenerateRandomToken_Prefix 测试Token前缀一致性
func TestGenerateRandomToken_Prefix(t *testing.T) {
	// 测试相同长度生成的Token前缀不固定（随机性验证）
	prefixes := make(map[byte]int)

	for i := 0; i < 100; i++ {
		token, err := GenerateRandomToken(64)
		require.NoError(t, err)

		if len(token) > 0 {
			prefixes[token[0]]++
		}
	}

	// 应该至少有5个不同的前缀字符（随机性验证）
	assert.GreaterOrEqual(t, len(prefixes), 5, "Should have variety in first character")
}

// TestGenerateRandomToken_ContainsNoNewlines 测试不包含换行
func TestGenerateRandomToken_ContainsNoNewlines(t *testing.T) {
	token, err := GenerateRandomToken(64)
	require.NoError(t, err)

	assert.NotContains(t, token, "\n", "Token should not contain newlines")
	assert.NotContains(t, token, "\r", "Token should not contain carriage returns")
}

// BenchmarkGenerateRandomToken 基准测试
func BenchmarkGenerateRandomToken(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateRandomToken(64)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateRandomTokenDifferentSizes 不同尺寸的基准测试
func BenchmarkGenerateRandomTokenDifferentSizes(b *testing.B) {
	sizes := []int{16, 32, 64, 128}

	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := GenerateRandomToken(size)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
