package cryptopackage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	// EncPrefixV1 AES-256-GCM 加密版本前缀
	EncPrefixV1 = "__ENC:v1:"
	// EncPrefixV2 版本号
	EncPrefixV2 = "__ENC:v2:"
	// MasterKeyFile 主密钥文件名
	MasterKeyFile = "master.key"
	// KeyDir 密钥存储目录
	KeyDir = "config"
)

// SecureKey 安全密钥封装，支持内存清理
type SecureKey struct {
	key  []byte
	lock sync.RWMutex
}

// Get 获取密钥副本
func (sk *SecureKey) Get() []byte {
	sk.lock.RLock()
	defer sk.lock.RUnlock()
	return sk.key
}

// Clear 清零内存并释放
func (sk *SecureKey) Clear() {
	sk.lock.Lock()
	defer sk.lock.Unlock()
	for i := range sk.key {
		sk.key[i] = 0
	}
	runtime.KeepAlive(sk.key)
	sk.key = nil
}

// MasterKeyManager 主密钥管理器
type MasterKeyManager struct {
	key      *SecureKey
	dataPath string
	source   string // "env" | "file" | "generated"
}

// NewMasterKeyManager 创建主密钥管理器
func NewMasterKeyManager(dataPath string) *MasterKeyManager {
	return &MasterKeyManager{
		dataPath: dataPath,
	}
}

// Initialize 初始化主密钥
// checkDataExists: 回调函数，检查数据库是否已有配置记录，返回 (exists bool, error)
func (m *MasterKeyManager) Initialize(checkDataExists func() (bool, error)) error {
	var key []byte
	var err error

	// 1. 检查环境变量
	if envKey := os.Getenv("CONFIG_ENCRYPTION_KEY"); envKey != "" {
		key, err = base64.StdEncoding.DecodeString(envKey)
		if err != nil {
			return fmt.Errorf("invalid CONFIG_ENCRYPTION_KEY: %w", err)
		}
		if len(key) != 32 {
			return fmt.Errorf("CONFIG_ENCRYPTION_KEY must be 32 bytes (base64 encoded), got %d bytes", len(key))
		}
		m.source = "env"
	} else {
		// 2. 检查文件
		keyPath := filepath.Join(m.dataPath, KeyDir, MasterKeyFile)
		if data, err := os.ReadFile(keyPath); err == nil {
			key, err = base64.StdEncoding.DecodeString(string(data))
			if err != nil {
				return fmt.Errorf("invalid master key file: %w", err)
			}
			if len(key) != 32 {
				return fmt.Errorf("master key must be 32 bytes, got %d bytes", len(key))
			}
			m.source = "file"
		} else {
			// 3. 检查数据库是否已有加密数据（防止二次生成灾难）
			if checkDataExists != nil {
				exists, err := checkDataExists()
				if err != nil {
					return fmt.Errorf("failed to check existing data: %w", err)
				}
				if exists {
					return errors.New("DETECTED_STALE_DATA: 数据库存在加密配置但找不到解密密钥！请恢复 master.key 文件或设置 CONFIG_ENCRYPTION_KEY 环境变量")
				}
			}

			// 4. 生成新密钥
			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("failed to generate master key: %w", err)
			}

			// 写入文件
			if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
				return fmt.Errorf("failed to create key directory: %w", err)
			}

			encoded := base64.StdEncoding.EncodeToString(key)
			if err := os.WriteFile(keyPath, []byte(encoded), 0600); err != nil {
				return fmt.Errorf("failed to write master key file: %w", err)
			}
			m.source = "generated"
		}
	}

	m.key = &SecureKey{key: key}
	m.printFingerprint()
	return nil
}

// printFingerprint 打印密钥指纹（SHA256 前8字节）
func (m *MasterKeyManager) printFingerprint() {
	hash := sha256.Sum256(m.key.Get())
	fingerprint := hex.EncodeToString(hash[:8])
	log.Printf("[Config] Master key source: %s", m.source)
	log.Printf("[Config] Master key fingerprint: %s", fingerprint)
}

// GetKey 获取主密钥
func (m *MasterKeyManager) GetKey() []byte {
	if m.key == nil {
		return nil
	}
	return m.key.Get()
}

// GetSource 获取密钥来源
func (m *MasterKeyManager) GetSource() string {
	return m.source
}

// ConfigEncryptor 配置加密器
type ConfigEncryptor struct {
	masterKey []byte
}

// NewConfigEncryptor 创建配置加密器
func NewConfigEncryptor(masterKey []byte) *ConfigEncryptor {
	return &ConfigEncryptor{masterKey: masterKey}
}

// Encrypt 加密字符串，返回带版本前缀的密文
func (e *ConfigEncryptor) Encrypt(plaintext string) string {
	if e.masterKey == nil || plaintext == "" {
		return plaintext
	}

	// 检查是否已加密
	if strings.HasPrefix(plaintext, EncPrefixV1) || strings.HasPrefix(plaintext, EncPrefixV2) {
		return plaintext
	}

	block, err := aes.NewCipher(e.masterKey)
	if err != nil {
		log.Printf("[ConfigEncryptor] Failed to create cipher: %v", err)
		return plaintext
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Printf("[ConfigEncryptor] Failed to create GCM: %v", err)
		return plaintext
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		log.Printf("[ConfigEncryptor] Failed to generate nonce: %v", err)
		return plaintext
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncPrefixV1 + base64.StdEncoding.EncodeToString(ciphertext)
}

// Decrypt 解密带版本前缀的密文
func (e *ConfigEncryptor) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, EncPrefixV1) && !strings.HasPrefix(ciphertext, EncPrefixV2) {
		return ciphertext, nil // 未加密，直接返回
	}

	if e.masterKey == nil {
		return "", errors.New("master key not available")
	}

	// 当前只支持 v1
	if strings.HasPrefix(ciphertext, EncPrefixV2) {
		return "", errors.New("unsupported encryption version v2")
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, EncPrefixV1))
	if err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	block, err := aes.NewCipher(e.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, cipherdata := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherdata, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt error: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted 检查字符串是否已加密
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, EncPrefixV1) || strings.HasPrefix(s, EncPrefixV2)
}
