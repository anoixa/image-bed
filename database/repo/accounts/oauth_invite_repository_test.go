package accounts

import (
	"context"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupInviteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.OAuthInvite{}))
	return db
}

func TestCreateInvite(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
	}
	require.NoError(t, repo.Create(ctx, invite))
	assert.NotZero(t, invite.ID)
}

func TestFindActiveInviteBySubject(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
	}
	require.NoError(t, repo.Create(ctx, invite))

	found, err := repo.FindActiveByProviderSubject(ctx, "github", "12345")
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.UserID)

	_, err = repo.FindActiveByProviderSubject(ctx, "github", "99999")
	assert.ErrorIs(t, err, ErrInviteNotFound)
}

func TestFindActiveInviteByVerifiedEmail(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Email:     "alice@example.com",
		CreatedBy: admin.ID,
	}
	require.NoError(t, repo.Create(ctx, invite))

	found, err := repo.FindActiveByProviderEmail(ctx, "github", "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.UserID)

	_, err = repo.FindActiveByProviderEmail(ctx, "github", "other@example.com")
	assert.ErrorIs(t, err, ErrInviteNotFound)
}

func TestConsumeInviteOnce(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
	}
	require.NoError(t, repo.Create(ctx, invite))

	require.NoError(t, repo.Consume(ctx, invite.ID))

	err := repo.Consume(ctx, invite.ID)
	assert.ErrorIs(t, err, ErrInviteConsumed)
}

func TestExpiredInviteCannotBeFound(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	past := time.Now().Add(-1 * time.Hour)
	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
		ExpiresAt: &past,
	}
	require.NoError(t, repo.Create(ctx, invite))

	_, err := repo.FindActiveByProviderSubject(ctx, "github", "12345")
	assert.ErrorIs(t, err, ErrInviteExpired)
}

func TestDeleteInvite(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	invite := &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
	}
	require.NoError(t, repo.Create(ctx, invite))

	require.NoError(t, repo.Delete(ctx, invite.ID))

	err := repo.Delete(ctx, invite.ID)
	assert.ErrorIs(t, err, ErrInviteNotFound)
}

func TestFindInvitesByUser(t *testing.T) {
	db := setupInviteTestDB(t)
	repo := NewOAuthInviteRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	admin := createTestUser(t, db, "admin")

	require.NoError(t, repo.Create(ctx, &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "github",
		Subject:   "12345",
		CreatedBy: admin.ID,
	}))
	require.NoError(t, repo.Create(ctx, &models.OAuthInvite{
		UserID:    user.ID,
		Provider:  "google",
		Email:     "alice@example.com",
		CreatedBy: admin.ID,
	}))

	invites, err := repo.FindByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, invites, 2)
}
