package crypto

import (
	"encoding/json"
	"fmt"

	cryptoutils "github.com/anoixa/image-bed/utils/crypto"
)

// Service 通用加密服务
type Service struct {
	keyManager *cryptoutils.MasterKeyManager
	encryptor  *cryptoutils.ConfigEncryptor
}

// NewService 创建加密服务
func NewService(dataPath string) *Service {
	return &Service{
		keyManager: cryptoutils.NewMasterKeyManager(dataPath),
	}
}

// Initialize 初始化密钥管理
func (s *Service) Initialize(checkDataExists func() (bool, error)) error {
	if err := s.keyManager.Initialize(checkDataExists); err != nil {
		return fmt.Errorf("failed to initialize master key: %w", err)
	}

	s.encryptor = cryptoutils.NewConfigEncryptor(s.keyManager.GetKey())
	return nil
}

// EncryptString 加密字符串
func (s *Service) EncryptString(plaintext string) string {
	if s.encryptor == nil {
		return plaintext
	}
	return s.encryptor.Encrypt(plaintext)
}

// DecryptString 解密字符串
func (s *Service) DecryptString(ciphertext string) (string, error) {
	if s.encryptor == nil {
		return ciphertext, nil
	}
	return s.encryptor.Decrypt(ciphertext)
}

// EncryptJSON 加密 JSON 数据
func (s *Service) EncryptJSON(data map[string]interface{}) (string, error) {
	if s.encryptor == nil {
		return "", fmt.Errorf("encryptor not initialized")
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}

	return s.encryptor.Encrypt(string(jsonData)), nil
}

// DecryptJSON 解密 JSON 数据
func (s *Service) DecryptJSON(ciphertext string) (map[string]interface{}, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("encryptor not initialized")
	}

	decrypted, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return data, nil
}

// IsEncrypted 检查是否已加密
func (s *Service) IsEncrypted(s2 string) bool {
	return cryptoutils.IsEncrypted(s2)
}
