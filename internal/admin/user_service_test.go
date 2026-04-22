package admin

import (
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	albumRepo "github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/keys"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}, &models.ApiToken{}, &models.Album{}))
	// Clean all tables before each test
	db.Exec("DELETE FROM devices")
	db.Exec("DELETE FROM api_tokens")
	db.Exec("DELETE FROM albums")
	db.Exec("DELETE FROM users")
	return db
}

func TestCreateUser(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	user, generatedPassword, err := svc.CreateUser("newuser", "password123", models.RoleUser)
	require.NoError(t, err)
	assert.Equal(t, "newuser", user.Username)
	assert.Equal(t, models.RoleUser, user.Role)
	assert.Empty(t, generatedPassword)
	assert.Equal(t, models.UserStatusActive, user.Status)

	// Verify password works
	stored, err := accounts.NewRepository(db).GetUserByUsername("newuser")
	require.NoError(t, err)
	ok, _ := cryptopackage.ComparePasswordAndHash("password123", stored.Password)
	assert.True(t, ok)
}

func TestCreateUserDuplicateUsername(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	_, _, err := svc.CreateUser("dup", "password123", models.RoleUser)
	require.NoError(t, err)

	_, _, err = svc.CreateUser("dup", "password456", models.RoleUser)
	assert.Error(t, err)
}

func TestCreateUserEmptyUsername(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	_, _, err := svc.CreateUser("", "password123", models.RoleUser)
	assert.Error(t, err)
}

func TestCreateUserShortPassword(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	_, _, err := svc.CreateUser("user", "12345", models.RoleUser)
	assert.Error(t, err)
}

func TestUpdateUserRole(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	svc := NewUserService(repo, nil, nil, nil, nil)

	user, _, _ := svc.CreateUser("target", "password123", models.RoleUser)

	err := svc.UpdateRole(user.ID, models.RoleAdmin)
	require.NoError(t, err)

	updated, _ := repo.GetUserByID(user.ID)
	assert.Equal(t, models.RoleAdmin, updated.Role)
}

func TestDisableUser(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	svc := NewUserService(repo, devicesRepo, nil, nil, nil)

	user, _, _ := svc.CreateUser("target", "password123", models.RoleUser)

	err := svc.UpdateStatus(user.ID, models.UserStatusDisabled)
	require.NoError(t, err)

	updated, _ := repo.GetUserByID(user.ID)
	assert.Equal(t, models.UserStatusDisabled, updated.Status)
}

func TestDisableUserClearsSessions(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	svc := NewUserService(repo, devicesRepo, nil, nil, nil)

	user, _, _ := svc.CreateUser("target", "password123", models.RoleUser)
	require.NoError(t, devicesRepo.CreateLoginDevice(user.ID, "dev1", "rt", time.Now().Add(time.Hour)))

	err := svc.UpdateStatus(user.ID, models.UserStatusDisabled)
	require.NoError(t, err)

	devices, _ := devicesRepo.GetDevicesByUser(user.ID)
	assert.Empty(t, devices)
}

func TestDisableUserDisablesAPITokens(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	keysRepo := keys.NewRepository(db)
	svc := NewUserService(repo, nil, keysRepo, nil, nil)

	user, _, _ := svc.CreateUser("token-user", "password123", models.RoleUser)
	require.NoError(t, keysRepo.CreateKey(&models.ApiToken{
		UserID:      user.ID,
		Description: "token-1",
		Token:       "hashed-token",
		IsActive:    true,
	}))

	err := svc.UpdateStatus(user.ID, models.UserStatusDisabled)
	require.NoError(t, err)

	tokens, err := keysRepo.GetAllApiTokensByUser(user.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.False(t, tokens[0].IsActive)
}

func TestDisableLastAdmin(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	svc := NewUserService(repo, nil, nil, nil, nil)

	admin := &models.User{
		Username: "admin",
		Password: "hash",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(admin))

	// Cannot disable the last admin
	err := svc.UpdateStatus(admin.ID, models.UserStatusDisabled)
	assert.Error(t, err)
}

func TestResetPassword(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	svc := NewUserService(repo, devicesRepo, nil, nil, nil)

	user, _, _ := svc.CreateUser("target", "password123", models.RoleUser)

	// Create a device session
	require.NoError(t, devicesRepo.CreateLoginDevice(user.ID, "dev1", "rt", time.Now().Add(time.Hour)))

	newPassword, err := svc.ResetPassword(user.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, newPassword)

	// Sessions should be cleared
	devices, _ := devicesRepo.GetDevicesByUser(user.ID)
	assert.Empty(t, devices)

	// New password should work
	stored, _ := repo.GetUserByID(user.ID)
	ok, _ := cryptopackage.ComparePasswordAndHash(newPassword, stored.Password)
	assert.True(t, ok)
}

func TestDeleteUser(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	svc := NewUserService(repo, nil, nil, nil, nil)

	user, _, _ := svc.CreateUser("target", "password123", models.RoleUser)

	err := svc.DeleteUser(user.ID)
	require.NoError(t, err)

	_, err = repo.GetUserByID(user.ID)
	assert.Error(t, err) // soft deleted
}

func TestDeleteUserWithOwnedDataRejected(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	svc := NewUserService(repo, nil, nil, nil, albumRepo.NewRepository(db))

	user, _, _ := svc.CreateUser("owner", "password123", models.RoleUser)
	require.NoError(t, db.Create(&models.Album{Name: "owned", UserID: user.ID}).Error)

	err := svc.DeleteUser(user.ID)
	assert.ErrorIs(t, err, ErrUserHasOwnedData)
}

func TestDeleteLastAdmin(t *testing.T) {
	db := setupAdminTestDB(t)
	repo := accounts.NewRepository(db)
	svc := NewUserService(repo, nil, nil, nil, nil)

	admin := &models.User{
		Username: "admin",
		Password: "hash",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(admin))

	err := svc.DeleteUser(admin.ID)
	assert.Error(t, err)
}

func TestListUsers(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	svc.CreateUser("a", "password123", models.RoleUser)
	svc.CreateUser("b", "password123", models.RoleUser)

	users, total, err := svc.ListUsers(1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, users, 2)
}

func TestCreateUserAutoGeneratesPasswordWhenEmpty(t *testing.T) {
	db := setupAdminTestDB(t)
	svc := NewUserService(accounts.NewRepository(db), nil, nil, nil, nil)

	user, generatedPassword, err := svc.CreateUser("generated", "", models.RoleUser)
	require.NoError(t, err)
	assert.Equal(t, "generated", user.Username)
	assert.NotEmpty(t, generatedPassword)
}
