package auth

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupKeyServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ApiToken{}))
	require.NoError(t, db.Exec("DELETE FROM api_tokens").Error)
	require.NoError(t, db.Exec("DELETE FROM users").Error)
	return db
}

func TestKeyServiceCreateKeyRejectsDisabledUser(t *testing.T) {
	db := setupKeyServiceTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	keysRepo := keys.NewRepository(db)
	svc := NewKeyService(keysRepo, accountsRepo)

	user := &models.User{
		Username: "disabled",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusDisabled,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	err := svc.CreateKey(&models.ApiToken{
		UserID:      user.ID,
		Token:       "hashed-token",
		TokenPrefix: "prefix",
		Description: "test",
		IsActive:    true,
	})
	assert.ErrorIs(t, err, ErrUserDisabled)
}

func TestKeyServiceListTokensRejectsDisabledUser(t *testing.T) {
	db := setupKeyServiceTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	keysRepo := keys.NewRepository(db)
	svc := NewKeyService(keysRepo, accountsRepo)

	user := &models.User{
		Username: "disabled",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusDisabled,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	_, err := svc.GetAllApiTokensByUser(user.ID)
	assert.ErrorIs(t, err, ErrUserDisabled)
}
