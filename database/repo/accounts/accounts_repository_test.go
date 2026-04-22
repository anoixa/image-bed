package accounts

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.Name()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&models.User{}))
	return db
}

func TestCountUsers(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	count, err := repo.CountUsers()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	require.NoError(t, repo.CreateUser(&models.User{
		Username: "alice",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}))

	count, err = repo.CountUsers()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestCountActiveAdmins(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	require.NoError(t, repo.CreateUser(&models.User{
		Username: "admin-active",
		Password: "hash",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusActive,
	}))
	require.NoError(t, repo.CreateUser(&models.User{
		Username: "admin-disabled",
		Password: "hash",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusDisabled,
	}))
	require.NoError(t, repo.CreateUser(&models.User{
		Username: "user-active",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}))

	count, err := repo.CountActiveAdmins()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestUpdateUserRole(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	user := &models.User{
		Username: "bob",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(user))

	err := repo.UpdateUserRole(user.ID, models.RoleAdmin)
	require.NoError(t, err)

	updated, err := repo.GetUserByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RoleAdmin, updated.Role)
}

func TestUpdateUserStatus(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	user := &models.User{
		Username: "charlie",
		Password: "hash",
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(user))

	err := repo.UpdateUserStatus(user.ID, models.UserStatusDisabled)
	require.NoError(t, err)

	updated, err := repo.GetUserByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, models.UserStatusDisabled, updated.Status)
}

func TestGetAllUsers(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, repo.CreateUser(&models.User{
			Username: name,
			Password: "hash",
			Role:     models.RoleUser,
			Status:   models.UserStatusActive,
		}))
	}

	users, total, err := repo.GetAllUsers(1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, users, 2)
}

func TestBackfillEmptyUserStatus(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	require.NoError(t, db.Exec(`INSERT INTO users (username, password, role, status, created_at, updated_at) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"legacy-user", "hash", models.RoleUser, "").Error)

	rows, err := repo.BackfillEmptyUserStatus(models.UserStatusActive)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	user, err := repo.GetUserByUsername("legacy-user")
	require.NoError(t, err)
	assert.Equal(t, models.UserStatusActive, user.Status)
}
