package user

import (
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}))
	return db
}

func TestChangePasswordRevokesAllUserSessions(t *testing.T) {
	db := setupUserServiceTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)

	hashedPassword, err := cryptopackage.GenerateFromPassword("old-password")
	require.NoError(t, err)

	user := &models.User{
		Username: "tester",
		Password: hashedPassword,
		Role:     models.RoleUser,
	}
	require.NoError(t, accountsRepo.CreateUser(user))
	require.NoError(t, devicesRepo.CreateLoginDevice(user.ID, "device-1", "refresh-token", time.Now().Add(time.Hour)))

	service := NewService(accountsRepo, devicesRepo)

	err = service.ChangePassword(ChangePasswordRequest{
		UserID:      user.ID,
		OldPassword: "old-password",
		NewPassword: "new-password",
	})
	require.NoError(t, err)

	devices, err := devicesRepo.GetDevicesByUser(user.ID)
	require.NoError(t, err)
	assert.Empty(t, devices)

	updatedUser, err := accountsRepo.GetUserByID(user.ID)
	require.NoError(t, err)
	ok, err := cryptopackage.ComparePasswordAndHash("new-password", updatedUser.Password)
	require.NoError(t, err)
	assert.True(t, ok)
}
