package auth

import (
	"errors"
	"testing"
	"time"

	appconfig "github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/internal/mfa"
	cryptopackage "github.com/anoixa/image-bed/utils/crypto"
	"github.com/pquerna/otp/totp"
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
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}, &models.UserTOTPSetting{}, &models.TwoFactorChallenge{}))
	db.Exec("DELETE FROM devices")
	db.Exec("DELETE FROM two_factor_challenges")
	db.Exec("DELETE FROM user_totp_settings")
	db.Exec("DELETE FROM users")
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

func TestLoginWith2FARequiresChallengeThenIssuesSession(t *testing.T) {
	db := setupLoginTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	twoFactorRepo := accounts.NewTwoFactorRepository(db)
	jwtSvc, err := NewJWTService(&appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "24h",
	}, nil, nil)
	require.NoError(t, err)
	crypto := testSecretCrypto{}
	svc := NewLoginServiceWith2FA(repo, devicesRepo, jwtSvc, twoFactorRepo, crypto)

	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	user := &models.User{
		Username: "totp-user",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(user))

	secret, _, err := mfa.GenerateTOTPSecret(user.Username)
	require.NoError(t, err)
	encrypted, err := crypto.EncryptString(secret)
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, db.Create(&models.UserTOTPSetting{
		UserID:          user.ID,
		SecretEncrypted: encrypted,
		Enabled:         true,
		EnabledAt:       &now,
	}).Error)

	result, err := svc.Login("totp-user", "password123")
	require.NoError(t, err)
	require.True(t, result.Requires2FA)
	require.NotEmpty(t, result.TwoFATicket)
	require.Equal(t, int64(300), result.TwoFAExpiresIn)

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	session, err := svc.Verify2FA(result.TwoFATicket, code)
	require.NoError(t, err)
	assert.NotEmpty(t, session.AccessToken)
	assert.NotEmpty(t, session.RefreshToken)

	_, err = svc.Verify2FA(result.TwoFATicket, code)
	assert.ErrorIs(t, err, ErrInvalidTwoFactorTicket)
}

func TestVerify2FARejectsReplayedTOTPCode(t *testing.T) {
	db := setupLoginTestDB(t)
	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	twoFactorRepo := accounts.NewTwoFactorRepository(db)
	jwtSvc, err := NewJWTService(&appconfig.Config{
		JWTSecret:          "test-secret-key-at-least-32-characters-long",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "24h",
	}, nil, nil)
	require.NoError(t, err)
	crypto := testSecretCrypto{}
	svc := NewLoginServiceWith2FA(repo, devicesRepo, jwtSvc, twoFactorRepo, crypto)

	hashedPassword, err := cryptopackage.GenerateFromPassword("password123")
	require.NoError(t, err)
	user := &models.User{
		Username: "replay-user",
		Password: hashedPassword,
		Role:     models.RoleUser,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(user))

	secret, _, err := mfa.GenerateTOTPSecret(user.Username)
	require.NoError(t, err)
	encrypted, err := crypto.EncryptString(secret)
	require.NoError(t, err)
	now := time.Now()
	code, err := totp.GenerateCode(secret, now)
	require.NoError(t, err)
	step, ok := mfa.ValidateTOTPCode(secret, code, now)
	require.True(t, ok)
	require.NoError(t, db.Create(&models.UserTOTPSetting{
		UserID:          user.ID,
		SecretEncrypted: encrypted,
		Enabled:         true,
		EnabledAt:       &now,
		LastUsedStep:    step,
	}).Error)

	result, err := svc.Login("replay-user", "password123")
	require.NoError(t, err)
	require.True(t, result.Requires2FA)

	_, err = svc.Verify2FA(result.TwoFATicket, code)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTwoFactorReplay))
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
