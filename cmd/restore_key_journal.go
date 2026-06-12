package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anoixa/image-bed/database/models"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"gorm.io/gorm"
)

const restoreKeyJournalConfigKey = "system:restore_master_key_pending"

const orphanStagedMasterKeyMaxAge = 24 * time.Hour

type restoreKeyJournal struct {
	StagedFile       string `json:"staged_file"`
	ExpectedChecksum string `json:"expected_checksum"`
}

func writeRestoreKeyJournal(tx *gorm.DB, plan masterKeyPlan) error {
	if !plan.write {
		return nil
	}
	if plan.tempPath == "" {
		return errors.New("cannot journal an unstaged master key")
	}
	sum := sha256.Sum256(plan.data)
	payload, err := json.Marshal(restoreKeyJournal{
		StagedFile:       filepath.Base(plan.tempPath),
		ExpectedChecksum: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return err
	}
	if err := tx.Unscoped().Where("key = ?", restoreKeyJournalConfigKey).Delete(&models.SystemConfig{}).Error; err != nil {
		return fmt.Errorf("failed to clear previous restore key journal: %w", err)
	}
	journal := &models.SystemConfig{
		Category:    models.ConfigCategorySystem,
		Name:        "Pending master key restore",
		Key:         restoreKeyJournalConfigKey,
		IsEnabled:   false,
		ConfigJSON:  string(payload),
		Description: "Internal crash-recovery marker; removed automatically",
	}
	if err := tx.Create(journal).Error; err != nil {
		return fmt.Errorf("failed to journal pending master key restore: %w", err)
	}
	return nil
}

func deleteRestoreKeyJournal(db *gorm.DB) error {
	return db.Unscoped().Where("key = ?", restoreKeyJournalConfigKey).Delete(&models.SystemConfig{}).Error
}

func cleanupOrphanStagedMasterKeys(dataPath string) {
	pattern := filepath.Join(dataPath, cryptopackage.KeyDir, ".master-key-*.tmp")
	matches, _ := filepath.Glob(pattern)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && time.Since(info.ModTime()) >= orphanStagedMasterKeyMaxAge {
			_ = os.Remove(match)
		}
	}
}

// recoverPendingMasterKey completes the filesystem half of a restore whose DB
// transaction committed before the process could atomically install master.key.
func recoverPendingMasterKey(db *gorm.DB, dataPath string) error {
	if !db.Migrator().HasTable(&models.SystemConfig{}) {
		return nil
	}
	var config models.SystemConfig
	err := db.Unscoped().Where("key = ?", restoreKeyJournalConfigKey).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cleanupOrphanStagedMasterKeys(dataPath)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read restore key journal: %w", err)
	}

	var journal restoreKeyJournal
	if err := json.Unmarshal([]byte(config.ConfigJSON), &journal); err != nil {
		return fmt.Errorf("invalid restore key journal: %w", err)
	}
	if journal.StagedFile != filepath.Base(journal.StagedFile) ||
		!strings.HasPrefix(journal.StagedFile, ".master-key-") ||
		!strings.HasSuffix(journal.StagedFile, ".tmp") {
		return fmt.Errorf("invalid staged master key filename %q", journal.StagedFile)
	}

	keyDir := filepath.Join(dataPath, cryptopackage.KeyDir)
	dest := filepath.Join(keyDir, cryptopackage.MasterKeyFile)
	staged := filepath.Join(keyDir, journal.StagedFile)
	if checksum, err := fileSHA256(dest); err == nil && checksum == journal.ExpectedChecksum {
		_ = os.Remove(staged)
		if err := deleteRestoreKeyJournal(db); err != nil {
			return fmt.Errorf("failed to clear completed restore key journal: %w", err)
		}
		return nil
	}

	checksum, err := fileSHA256(staged)
	if err != nil {
		return fmt.Errorf("pending restore requires staged master key %s: %w", staged, err)
	}
	if checksum != journal.ExpectedChecksum {
		return fmt.Errorf("staged master key checksum mismatch: expected %s, got %s", journal.ExpectedChecksum, checksum)
	}
	if err := os.Rename(staged, dest); err != nil {
		return fmt.Errorf("failed to recover pending master key: %w", err)
	}
	if err := syncDirectory(keyDir); err != nil {
		return err
	}
	if err := deleteRestoreKeyJournal(db); err != nil {
		return fmt.Errorf("failed to clear recovered restore key journal: %w", err)
	}
	restoreLog.Warnf("Recovered master key installation from an interrupted restore")
	return nil
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("failed to open directory for sync: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync directory: %w", err)
	}
	return nil
}
