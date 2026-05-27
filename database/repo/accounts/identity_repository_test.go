package accounts

import (
	"context"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.UserIdentity{}))
	return db
}

func createTestUser(t *testing.T, db *gorm.DB, username string) *models.User {
	t.Helper()
	user := &models.User{
		Username: username,
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func TestCreateIdentity(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")

	identity := &models.UserIdentity{
		UserID:        user.ID,
		Provider:      "github",
		Subject:       "12345",
		Username:      "alicegh",
		Email:         "alice@example.com",
		EmailVerified: true,
	}
	require.NoError(t, repo.Create(ctx, identity))
	assert.NotZero(t, identity.ID)
}

func TestUniqueProviderSubject(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user1 := createTestUser(t, db, "alice")
	user2 := createTestUser(t, db, "bob")

	identity1 := &models.UserIdentity{
		UserID:   user1.ID,
		Provider: "github",
		Subject:  "12345",
	}
	require.NoError(t, repo.Create(ctx, identity1))

	identity2 := &models.UserIdentity{
		UserID:   user2.ID,
		Provider: "github",
		Subject:  "12345",
	}
	err := repo.Create(ctx, identity2)
	assert.Error(t, err, "duplicate provider+subject should fail")
}

func TestUniqueUserProvider(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")

	identity1 := &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "111",
	}
	require.NoError(t, repo.Create(ctx, identity1))

	identity2 := &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "222",
	}
	err := repo.Create(ctx, identity2)
	assert.Error(t, err, "duplicate user+provider should fail")
}

func TestFindByProviderSubject(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	require.NoError(t, repo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))

	found, err := repo.FindByProviderSubject(ctx, "github", "12345")
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.UserID)
	assert.Equal(t, "12345", found.Subject)

	_, err = repo.FindByProviderSubject(ctx, "github", "99999")
	assert.ErrorIs(t, err, ErrIdentityNotFound)
}

func TestFindByUser(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	require.NoError(t, repo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))
	require.NoError(t, repo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "google",
		Subject:  "67890",
	}))

	identities, err := repo.FindByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, identities, 2)
}

func TestDeleteIdentity(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	require.NoError(t, repo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))

	require.NoError(t, repo.Delete(ctx, user.ID, "github"))

	_, err := repo.FindByProviderSubject(ctx, "github", "12345")
	assert.ErrorIs(t, err, ErrIdentityNotFound)
}

func TestCountByUser(t *testing.T) {
	db := setupIdentityTestDB(t)
	repo := NewIdentityRepository(db)
	ctx := context.Background()

	user := createTestUser(t, db, "alice")
	require.NoError(t, repo.Create(ctx, &models.UserIdentity{
		UserID:   user.ID,
		Provider: "github",
		Subject:  "12345",
	}))

	count, err := repo.CountByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
