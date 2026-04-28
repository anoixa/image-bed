package cryptopackage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/anoixa/image-bed/utils"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var configLog = utils.ForModule("Config")

const (
	// EncPrefixV1 AES-256-GCM
	EncPrefixV1 = "__ENC:v1:"
	// EncPrefixV2 版本号
	EncPrefixV2 = "__ENC:v2:"
	// MasterKeyFile 主密钥文件名
	MasterKeyFile = "master.key"
	// KeyDir 密钥存储目录
	KeyDir = "config"

	configEncryptionHKDFSalt = "github.com/anoixa/image-bed/config-encryption/v1"
	configEncryptionHKDFInfo = "system-config/aes-256-gcm"
)

// SecureKey 安全密钥封装
type SecureKey struct {
	key  []byte
	lock sync.RWMutex
}

// Get 获取密钥副本
func (sk *SecureKey) Get() []byte {
	sk.lock.RLock()
	defer sk.lock.RUnlock()
	if sk.key == nil {
		return nil
	}
	return append([]byte(nil), sk.key...)
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
func (m *MasterKeyManager) Initialize(checkDataExists func() (bool, error)) error {
	var key []byte
	var err error

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
			if checkDataExists != nil {
				exists, err := checkDataExists()
				if err != nil {
					return fmt.Errorf("failed to check existing data: %w", err)
				}
				if exists {
					return errors.New("DETECTED_STALE_DATA: 数据库存在加密配置但找不到解密密钥！请恢复 master.key 文件或设置 CONFIG_ENCRYPTION_KEY 环境变量")
				}
			}

			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("failed to generate master key: %w", err)
			}

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

// printFingerprint 打印密钥指纹
func (m *MasterKeyManager) printFingerprint() {
	hash := sha256.Sum256(m.key.Get())
	fingerprint := hex.EncodeToString(hash[:8])
	configLog.Infof("Master key source: %s", m.source)
	configLog.Infof("Master key fingerprint: %s", fingerprint)
}

// GetKey 获取主密钥
func (m *MasterKeyManager) GetKey() []byte {
	if m.key == nil {
		return nil
	}
	return m.key.Get()
}

// GetConfigEncryptionKey 获取配置加密专用密钥。
// CONFIG_ENCRYPTION_KEY 被视为最终加密密钥；文件 master.key 则通过 HKDF 派生用途专属密钥。
func (m *MasterKeyManager) GetConfigEncryptionKey() ([]byte, error) {
	key := m.GetKey()
	if key == nil {
		return nil, errors.New("master key not available")
	}
	if m.source == "env" {
		return key, nil
	}
	return deriveConfigEncryptionKey(key)
}

// GetLegacyConfigEncryptionKeys 返回仅用于解密历史密文的旧密钥。
func (m *MasterKeyManager) GetLegacyConfigEncryptionKeys() [][]byte {
	if m.source == "env" {
		return nil
	}
	key := m.GetKey()
	if key == nil {
		return nil
	}
	return [][]byte{key}
}

// GetSource 获取密钥来源
func (m *MasterKeyManager) GetSource() string {
	return m.source
}

func deriveConfigEncryptionKey(masterKey []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, masterKey, []byte(configEncryptionHKDFSalt), configEncryptionHKDFInfo, 32)
}

// ConfigEncryptor 配置加密器
type ConfigEncryptor struct {
	primaryKey   []byte
	fallbackKeys [][]byte
}

// NewConfigEncryptor 创建配置加密器
func NewConfigEncryptor(key []byte) *ConfigEncryptor {
	return NewConfigEncryptorWithFallback(key)
}

// NewConfigEncryptorWithFallback 创建带历史解密密钥的配置加密器。
func NewConfigEncryptorWithFallback(primaryKey []byte, fallbackKeys ...[]byte) *ConfigEncryptor {
	encryptor := &ConfigEncryptor{
		primaryKey: append([]byte(nil), primaryKey...),
	}
	for _, key := range fallbackKeys {
		if len(key) > 0 {
			encryptor.fallbackKeys = append(encryptor.fallbackKeys, append([]byte(nil), key...))
		}
	}
	return encryptor
}

// Encrypt 加密字符串，返回带版本前缀的密文
func (e *ConfigEncryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return plaintext, nil
	}

	if e.primaryKey == nil {
		return "", errors.New("encryption key not available")
	}

	if strings.HasPrefix(plaintext, EncPrefixV1) || strings.HasPrefix(plaintext, EncPrefixV2) {
		return plaintext, nil
	}

	block, err := aes.NewCipher(e.primaryKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncPrefixV1 + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密带版本前缀的密文
func (e *ConfigEncryptor) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, EncPrefixV1) && !strings.HasPrefix(ciphertext, EncPrefixV2) {
		return ciphertext, nil // 未加密，直接返回
	}

	if e.primaryKey == nil {
		return "", errors.New("encryption key not available")
	}

	// 当前只支持 v1
	if strings.HasPrefix(ciphertext, EncPrefixV2) {
		return "", errors.New("unsupported encryption version v2")
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, EncPrefixV1))
	if err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	nonceSize, err := nonceSizeForKey(e.primaryKey)
	if err != nil {
		return "", err
	}
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, cipherdata := data[:nonceSize], data[nonceSize:]
	plaintext, err := decryptWithKey(e.primaryKey, nonce, cipherdata)
	if err == nil {
		return plaintext, nil
	}

	for _, key := range e.fallbackKeys {
		if plaintext, fallbackErr := decryptWithKey(key, nonce, cipherdata); fallbackErr == nil {
			return plaintext, nil
		}
	}

	return "", fmt.Errorf("decrypt error: %w", err)
}

func decryptWithKey(key, nonce, cipherdata []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, cipherdata, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func nonceSizeForKey(key []byte) (int, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm.NonceSize(), nil
}

// IsEncrypted 检查字符串是否已加密
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, EncPrefixV1) || strings.HasPrefix(s, EncPrefixV2)
}

// GenerateRandomKey 生成指定字节长度的随机密钥，返回 base64 编码的字符串
func GenerateRandomKey(length int) (string, error) {
	key := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
