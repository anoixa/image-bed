package auth

import (
	"testing"

	appconfig "github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupLoginTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}))
	db.Exec("DELETE FROM devices")
	db.Exec("DELETE FROM users")
	return db
}

func TestLogin_DisabledUserRejected(t *testing.T) {
	db := setupLoginTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	jwtSvc, err := NewJWTService(&appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "24h",
	}, nil, nil)
	require.NoError(t, err)
	svc := NewLoginService(repo, devicesRepo, jwtSvc)

	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	require.NoError(t, repo.CreateUser(&models.User{
		Username: "disabled-user",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusDisabled,
	}))

	_, err = svc.Login("disabled-user", "password123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account disabled")
}

func TestLogin_ActiveUserSucceeds(t *testing.T) {
	db := setupLoginTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	jwtSvc, err := NewJWTService(&appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "24h",
	}, nil, nil)
	require.NoError(t, err)
	svc := NewLoginService(repo, devicesRepo, jwtSvc)

	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	require.NoError(t, repo.CreateUser(&models.User{
		Username: "active-user",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}))

	result, err := svc.Login("active-user", "password123")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "active-user", result.User.Username)
}

func TestRefreshToken_DisabledUserRejected(t *testing.T) {
	db := setupLoginTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	jwtSvc, err := NewJWTService(&appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "24h",
	}, nil, nil)
	require.NoError(t, err)
	svc := NewLoginService(repo, devicesRepo, jwtSvc)

	// Create active user and login
	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	require.NoError(t, repo.CreateUser(&models.User{
		Username: "to-disable",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}))

	result, err := svc.Login("to-disable", "password123")
	require.NoError(t, err)

	// Now disable the user
	require.NoError(t, repo.UpdateUserStatus(result.User.ID, models.UserStatusDisabled))

	// Refresh should fail
	_, err = svc.RefreshToken(result.RefreshToken, result.DeviceID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account disabled")
}
