package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

var (
	ErrTOTPSettingNotFound = errors.New("totp setting not found")
	ErrChallengeNotFound   = errors.New("two factor challenge not found")
)

// TwoFactorRepository manages TOTP settings and login challenges.
type TwoFactorRepository struct {
	db *gorm.DB
}

func NewTwoFactorRepository(db *gorm.DB) *TwoFactorRepository {
	return &TwoFactorRepository{db: db}
}

func (r *TwoFactorRepository) DB() *gorm.DB {
	return r.db
}

func (r *TwoFactorRepository) WithDB(db *gorm.DB) *TwoFactorRepository {
	return &TwoFactorRepository{db: db}
}

func (r *TwoFactorRepository) GetTOTPSetting(ctx context.Context, userID uint) (*models.UserTOTPSetting, error) {
	var setting models.UserTOTPSetting
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTOTPSettingNotFound
		}
		return nil, err
	}
	return &setting, nil
}

func (r *TwoFactorRepository) IsTOTPEnabled(ctx context.Context, userID uint) (bool, error) {
	setting, err := r.GetTOTPSetting(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrTOTPSettingNotFound) {
			return false, nil
		}
		return false, err
	}
	return setting.Enabled && setting.SecretEncrypted != "", nil
}

func (r *TwoFactorRepository) SavePendingTOTP(ctx context.Context, userID uint, encryptedSecret string, expiresAt time.Time) (*models.UserTOTPSetting, error) {
	setting, err := r.GetTOTPSetting(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrTOTPSettingNotFound) {
			return nil, err
		}
		setting = &models.UserTOTPSetting{UserID: userID}
	}

	setting.PendingSecretEncrypted = encryptedSecret
	setting.PendingExpiresAt = &expiresAt
	if err := r.db.WithContext(ctx).Save(setting).Error; err != nil {
		return nil, err
	}
	return setting, nil
}

func (r *TwoFactorRepository) EnableTOTP(ctx context.Context, userID uint, encryptedSecret string, enabledAt time.Time, lastUsedStep int64) error {
	return r.db.WithContext(ctx).Model(&models.UserTOTPSetting{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"secret_encrypted":         encryptedSecret,
			"pending_secret_encrypted": "",
			"pending_expires_at":       nil,
			"enabled":                  true,
			"enabled_at":               enabledAt,
			"last_used_step":           lastUsedStep,
		}).Error
}

func (r *TwoFactorRepository) ResetTOTP(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Model(&models.UserTOTPSetting{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"secret_encrypted":         "",
			"pending_secret_encrypted": "",
			"pending_expires_at":       nil,
			"enabled":                  false,
			"enabled_at":               nil,
			"last_used_step":           int64(0),
		}).Error
}

func (r *TwoFactorRepository) CreateChallenge(ctx context.Context, challenge *models.TwoFactorChallenge) error {
	return r.db.WithContext(ctx).Create(challenge).Error
}

func (r *TwoFactorRepository) DeleteExpiredChallenges(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ? OR consumed_at IS NOT NULL", before).
		Delete(&models.TwoFactorChallenge{}).Error
}
