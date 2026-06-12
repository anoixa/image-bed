package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecoverPendingMasterKeyCompletesCommittedRestore(t *testing.T) {
	db := newRestoreTestDB(t)
	dataPath := t.TempDir()
	keyData := base64Key(t, 0xA5)
	plan := masterKeyPlan{
		write: true,
		dest:  filepath.Join(dataPath, cryptopackage.KeyDir, cryptopackage.MasterKeyFile),
		data:  keyData,
	}
	plan, err := stageMasterKeyFile(plan)
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return writeRestoreKeyJournal(tx, plan)
	}))

	require.NoError(t, recoverPendingMasterKey(db, dataPath))
	got, err := os.ReadFile(plan.dest)
	require.NoError(t, err)
	assert.Equal(t, keyData, got)
	assert.NoFileExists(t, plan.tempPath)

	var count int64
	require.NoError(t, db.Unscoped().Table("system_configs").Where("key = ?", restoreKeyJournalConfigKey).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRecoverPendingMasterKeyRemovesUncommittedOrphan(t *testing.T) {
	db := newRestoreTestDB(t)
	dataPath := t.TempDir()
	keyDir := filepath.Join(dataPath, cryptopackage.KeyDir)
	require.NoError(t, os.MkdirAll(keyDir, 0700))
	orphan := filepath.Join(keyDir, ".master-key-orphan.tmp")
	require.NoError(t, os.WriteFile(orphan, base64Key(t, 0x12), 0600))
	old := time.Now().Add(-orphanStagedMasterKeyMaxAge - time.Hour)
	require.NoError(t, os.Chtimes(orphan, old, old))

	require.NoError(t, recoverPendingMasterKey(db, dataPath))
	assert.NoFileExists(t, orphan)
}

func TestRecoverPendingMasterKeyKeepsFreshStagingFile(t *testing.T) {
	db := newRestoreTestDB(t)
	dataPath := t.TempDir()
	keyDir := filepath.Join(dataPath, cryptopackage.KeyDir)
	require.NoError(t, os.MkdirAll(keyDir, 0700))
	staging := filepath.Join(keyDir, ".master-key-active.tmp")
	require.NoError(t, os.WriteFile(staging, base64Key(t, 0x34), 0600))

	require.NoError(t, recoverPendingMasterKey(db, dataPath))
	assert.FileExists(t, staging, "a concurrent restore may still own a fresh staging file")
}
