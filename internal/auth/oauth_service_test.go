package auth

import (
	"context"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupOAuthServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.UserIdentity{}))
	cleanup := db.Session(&gorm.Session{AllowGlobalUpdate: true})
	require.NoError(t, cleanup.Unscoped().Delete(&models.UserIdentity{}).Error)
	require.NoError(t, cleanup.Unscoped().Delete(&models.User{}).Error)
	return db
}

func TestUnlinkIdentityRefusesLastOAuthMethodWhenPasswordLoginDisabled(t *testing.T) {
	db := setupOAuthServiceTestDB(t)
	ctx := context.Background()
	accountsRepo := accounts.NewRepository(db)
	identityRepo := accounts.NewIdentityRepository(db)

	user := &models.User{
		Username: "oauth-only",
		Password: "existing-password-hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, accountsRepo.CreateUser(user))
	require.NoError(t, identityRepo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))

	svc := NewOAuthService([]byte("test-secret"), nil, identityRepo, accountsRepo, false)
	err := svc.UnlinkIdentity(ctx, user.ID, "github")
	assert.ErrorIs(t, err, ErrLastLoginMethod)

	_, err = identityRepo.FindByUserProvider(ctx, user.ID, "github")
	assert.NoError(t, err, "identity should remain linked after refused unlink")
}

func TestUnlinkIdentityAllowsPasswordFallbackWhenPasswordLoginEnabled(t *testing.T) {
	db := setupOAuthServiceTestDB(t)
	ctx := context.Background()
	accountsRepo := accounts.NewRepository(db)
	identityRepo := accounts.NewIdentityRepository(db)

	user := &models.User{
		Username: "password-fallback",
		Password: "existing-password-hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, accountsRepo.CreateUser(user))
	require.NoError(t, identityRepo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))

	svc := NewOAuthService([]byte("test-secret"), nil, identityRepo, accountsRepo, true)
	require.NoError(t, svc.UnlinkIdentity(ctx, user.ID, "github"))

	_, err := identityRepo.FindByUserProvider(ctx, user.ID, "github")
	assert.ErrorIs(t, err, accounts.ErrIdentityNotFound)
}

func TestOAuthLoginRequiresExistingIdentityBinding(t *testing.T) {
	db := setupOAuthServiceTestDB(t)
	ctx := context.Background()
	accountsRepo := accounts.NewRepository(db)
	identityRepo := accounts.NewIdentityRepository(db)

	user := &models.User{
		Username: "manual-user",
		Password: "existing-password-hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	svc := NewOAuthService([]byte("test-secret"), nil, identityRepo, accountsRepo, true)
	_, _, err := svc.handleLogin(ctx, "github", &ExternalIdentity{
		Subject:       "unlinked-subject",
		Username:      "github-user",
		Email:         "user@example.com",
		EmailVerified: true,
	}, "/")
	assert.ErrorIs(t, err, ErrIdentityNotLinked)

	identities, err := identityRepo.FindByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Empty(t, identities)
}
