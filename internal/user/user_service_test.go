package user

import (
	"errors"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/pquerna/otp/totp"
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
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}, &models.UserTOTPSetting{}))
	return db
}

type testSecretCrypto struct{}

func (testSecretCrypto) EncryptString(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (testSecretCrypto) DecryptString(ciphertext string) (string, error) {
	if len(ciphertext) >= 4 && ciphertext[:4] == "enc:" {
		return ciphertext[4:], nil
	}
	return ciphertext, nil
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

func TestGetCurrentUser(t *testing.T) {
	db := setupUserServiceTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	service := NewService(accountsRepo, nil)

	user := &models.User{
		Username: "tester",
		Password: "hashed",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusDisabled,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	currentUser, err := service.GetCurrentUser(user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, currentUser.ID)
	assert.Equal(t, "tester", currentUser.Username)
	assert.Equal(t, models.RoleAdmin, currentUser.Role)
	assert.Equal(t, models.UserStatusDisabled, currentUser.Status)
}

func TestSetupAndEnable2FA(t *testing.T) {
	db := setupUserServiceTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	twoFactorRepo := accounts.NewTwoFactorRepository(db)
	crypto := testSecretCrypto{}
	service := NewServiceWith2FA(accountsRepo, nil, twoFactorRepo, crypto)

	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	user := &models.User{
		Username: "totp-user",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	_, err = service.Setup2FA(Setup2FARequest{UserID: user.ID, CurrentPassword: "wrong"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidOldPassword))

	setup, err := service.Setup2FA(Setup2FARequest{UserID: user.ID, CurrentPassword: "password123"})
	require.NoError(t, err)
	require.NotEmpty(t, setup.URI)
	require.NotEmpty(t, setup.Secret)

	setting, err := twoFactorRepo.GetTOTPSetting(t.Context(), user.ID)
	require.NoError(t, err)
	assert.False(t, setting.Enabled)
	assert.NotEqual(t, setup.Secret, setting.PendingSecretEncrypted)
	assert.NotEmpty(t, setting.PendingSecretEncrypted)

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, service.Enable2FA(user.ID, code))

	enabled, err := service.Get2FAStatus(user.ID)
	require.NoError(t, err)
	assert.True(t, enabled)
}
