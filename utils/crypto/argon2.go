package cryptopackage

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"io"
	"strings"
)

// 定义 Argon2id 的推荐参数
const (
	// memory 是以 KiB 为单位的内存消耗
	// 对于交互式应用，通常推荐 64MB (65536 KiB) 到 256MB (262144 KiB)
	// 根据你的服务器资源和安全需求调整。
	argon2Memory uint32 = 65536 // 64 MB

	// iterations 是迭代次数（时间成本）
	// 通常推荐 2 到 4 次，更高的迭代次数会增加计算时间，降低暴力破解速度。
	argon2Iterations uint32 = 2

	// parallelism 是并行度（线程数）
	// 通常设置为 CPU 核心数或其一半。
	argon2Parallelism uint8 = 4

	// saltLength 是盐值的字节长度。
	// 推荐至少 16 字节。
	argon2SaltLength uint32 = 16

	// keyLength 是生成的哈希（密码摘要）的字节长度。
	// 推荐至少 32 字节。
	argon2KeyLength uint32 = 32
)

// GenerateFromPassword 使用 Argon2id 算法哈希密码
// 返回的字符串包含所有必要的参数，可以安全地存储在数据库中。
func GenerateFromPassword(password string) (string, error) {
	// 生成随机盐值
	salt := make([]byte, argon2SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// 使用 Argon2id 哈希密码
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)

	// 将盐值和哈希值编码为 Base64 字符串
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// 格式化为推荐的 Argon2id 字符串格式
	// $argon2id$v={version}$m={memory},t={iterations},p={parallelism}${salt}${hash}
	encodedHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Iterations, argon2Parallelism, b64Salt, b64Hash)

	return encodedHash, nil
}

// ComparePasswordAndHash 比较明文密码和 Argon2id 哈希值
func ComparePasswordAndHash(password, encodedHash string) (bool, error) {
	// 使用 strings.Split 更安全地解析哈希字符串
	parts := strings.Split(encodedHash, "$")

	// 检查分割后的部分是否正确
	// 期望格式: "", "argon2id", "v=...", "m=...,t=...,p=...", "salt", "hash"
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("invalid Argon2id hash format: incorrect number of parts or missing prefix")
	}

	var version int
	// 解析版本号部分
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return false, fmt.Errorf("invalid Argon2id version format: %w", err)
	}

	var memory, iterations, parallelism uint32
	// 解析成本参数部分
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false, fmt.Errorf("invalid Argon2id cost parameters format: %w", err)
	}

	// 盐值和哈希值现在是 Base64 编码的字符串
	b64SaltStr := parts[4]
	b64HashStr := parts[5]

	// Base64 解码盐值和哈希值
	decodedSalt, err := base64.RawStdEncoding.DecodeString(b64SaltStr)
	if err != nil {
		return false, fmt.Errorf("failed to decode salt: %w", err)
	}
	decodedHash, err := base64.RawStdEncoding.DecodeString(b64HashStr)
	if err != nil {
		return false, fmt.Errorf("failed to decode hash: %w", err)
	}

	// 重新计算给定密码的哈希值
	// 注意: len(decodedHash) 作为 keyLength，确保与原始哈希的长度一致
	computedHash := argon2.IDKey([]byte(password), decodedSalt, iterations, memory, uint8(parallelism), uint32(len(decodedHash)))

	// 使用 constant-time 比较，防止定时攻击
	if subtle.ConstantTimeCompare(decodedHash, computedHash) == 1 {
		return true, nil
	}
	return false, nil
}
