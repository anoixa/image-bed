package cmd

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
)

const (
	archiveIntegritySalt = "github.com/anoixa/image-bed/backup-integrity/v1"
	archiveIntegrityInfo = "metadata-and-table-checksums/hmac-sha256"
)

func archiveContainsKeyDependentData(meta *backupMetadata) bool {
	for _, table := range masterKeyDependentTables {
		if meta.RecordCount[table] > 0 {
			return true
		}
	}
	return false
}

func encryptionKeyFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

func deriveArchiveIntegrityKey(configKey []byte) ([]byte, error) {
	if len(configKey) != 32 {
		return nil, fmt.Errorf("config encryption key must be 32 bytes, got %d", len(configKey))
	}
	return hkdf.Key(sha256.New, configKey, []byte(archiveIntegritySalt), archiveIntegrityInfo, 32)
}

func metadataMAC(meta *backupMetadata, key []byte) (string, error) {
	clone := *meta
	clone.ArchiveMAC = ""
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("failed to serialize archive metadata for authentication: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	if _, err := mac.Write(payload); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func applyArchiveSecurity(meta *backupMetadata, configKey []byte) error {
	meta.EncryptionKeyFingerprint = encryptionKeyFingerprint(configKey)
	integrityKey, err := deriveArchiveIntegrityKey(configKey)
	if err != nil {
		return err
	}
	meta.ArchiveMAC, err = metadataMAC(meta, integrityKey)
	return err
}

func configKeyFromBundledMasterKey(extractDir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(extractDir, masterKeyArchiveEntry))
	if err != nil {
		return nil, fmt.Errorf("failed to read bundled master key for archive authentication: %w", err)
	}
	raw, err := decodeMasterKeyFile(data)
	if err != nil {
		return nil, err
	}
	return cryptopackage.DeriveConfigEncryptionKey(raw)
}

func decodeMasterKeyFile(data []byte) ([]byte, error) {
	raw, err := decodeBase64Key(data)
	if err != nil {
		return nil, fmt.Errorf("master key file is invalid: %w", err)
	}
	return raw, nil
}

func decodeBase64Key(data []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("key must decode to 32 bytes, got %d", len(raw))
	}
	return raw, nil
}

func archiveVersion(version string) (major, minor int, err error) {
	parts := strings.SplitN(version, ".", 3)
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("malformed archive version %q", version)
	}
	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("malformed archive version %q", version)
		}
	}
	return major, minor, nil
}

func validateArchiveSecurity(extractDir, dataPath string, meta *backupMetadata) error {
	if !archiveContainsKeyDependentData(meta) {
		return nil
	}

	major, minor, err := archiveVersion(meta.Version)
	if err != nil {
		return err
	}
	if major < archiveMajorV2 || (major == archiveMajorV2 && minor < 1) {
		restoreLog.Warnf("archive %s predates encryption-key fingerprint and HMAC validation", meta.Version)
		return nil
	}
	if meta.EncryptionKeyFingerprint == "" || meta.ArchiveMAC == "" {
		return errors.New("archive containing encrypted data is missing its encryption-key fingerprint or authentication code")
	}

	var configKey []byte
	if meta.MasterKeyIncluded {
		configKey, err = configKeyFromBundledMasterKey(extractDir)
	} else {
		configKey, _, err = cryptopackage.LoadExistingConfigEncryptionKey(dataPath)
	}
	if err != nil {
		return fmt.Errorf("cannot validate encrypted archive without its original config encryption key: %w", err)
	}
	if got := encryptionKeyFingerprint(configKey); !hmac.Equal([]byte(got), []byte(meta.EncryptionKeyFingerprint)) {
		return fmt.Errorf("config encryption key fingerprint mismatch: archive requires %s, current key is %s", meta.EncryptionKeyFingerprint, got)
	}

	integrityKey, err := deriveArchiveIntegrityKey(configKey)
	if err != nil {
		return err
	}
	want, err := metadataMAC(meta, integrityKey)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(meta.ArchiveMAC)) {
		return errors.New("archive authentication failed: metadata or table checksums were modified")
	}
	return nil
}
