package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

var (
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteConsumed = errors.New("invite already consumed")
	ErrInviteExpired  = errors.New("invite expired")
)

// OAuthInviteRepository manages OAuth login invites.
type OAuthInviteRepository struct {
	db *gorm.DB
}

// NewOAuthInviteRepository creates a new OAuthInviteRepository.
func NewOAuthInviteRepository(db *gorm.DB) *OAuthInviteRepository {
	return &OAuthInviteRepository{db: db}
}

// DB returns the underlying database connection.
func (r *OAuthInviteRepository) DB() *gorm.DB {
	return r.db
}

// Create inserts a new invite.
func (r *OAuthInviteRepository) Create(ctx context.Context, invite *models.OAuthInvite) error {
	return r.db.WithContext(ctx).Create(invite).Error
}

// FindActiveByProviderSubject finds a consumable invite by provider and subject.
func (r *OAuthInviteRepository) FindActiveByProviderSubject(ctx context.Context, provider, subject string) (*models.OAuthInvite, error) {
	var invite models.OAuthInvite
	err := r.db.WithContext(ctx).
		Where("provider = ? AND subject = ? AND used_at IS NULL", provider, subject).
		First(&invite).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}
	if invite.IsExpired() {
		return nil, ErrInviteExpired
	}
	return &invite, nil
}

// FindActiveByProviderEmail finds a consumable invite by provider and email.
func (r *OAuthInviteRepository) FindActiveByProviderEmail(ctx context.Context, provider, email string) (*models.OAuthInvite, error) {
	var invite models.OAuthInvite
	err := r.db.WithContext(ctx).
		Where("provider = ? AND email = ? AND used_at IS NULL", provider, email).
		First(&invite).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}
	if invite.IsExpired() {
		return nil, ErrInviteExpired
	}
	return &invite, nil
}

// Consume marks an invite as used at the current time.
func (r *OAuthInviteRepository) Consume(ctx context.Context, inviteID uint) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&models.OAuthInvite{}).
		Where("id = ? AND used_at IS NULL", inviteID).
		Update("used_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInviteConsumed
	}
	return nil
}

// FindByUser returns all invites for a given user.
func (r *OAuthInviteRepository) FindByUser(ctx context.Context, userID uint) ([]*models.OAuthInvite, error) {
	var invites []*models.OAuthInvite
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&invites).Error
	return invites, err
}

// Delete removes an invite.
func (r *OAuthInviteRepository) Delete(ctx context.Context, inviteID uint) error {
	result := r.db.WithContext(ctx).Delete(&models.OAuthInvite{}, inviteID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInviteNotFound
	}
	return nil
}

// WithContext returns a copy of the repository with the given context.
func (r *OAuthInviteRepository) WithContext(ctx context.Context) *OAuthInviteRepository {
	return &OAuthInviteRepository{db: r.db.WithContext(ctx)}
}
