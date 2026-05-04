package accounts

import (
	"context"
	"errors"

	"github.com/anoixa/image-bed/database/models"
	"gorm.io/gorm"
)

var (
	ErrIdentityNotFound     = errors.New("identity not found")
	ErrIdentityAlreadyBound = errors.New("identity already bound to another user")
)

// IdentityRepository manages OAuth identity bindings.
type IdentityRepository struct {
	db *gorm.DB
}

// NewIdentityRepository creates a new IdentityRepository.
func NewIdentityRepository(db *gorm.DB) *IdentityRepository {
	return &IdentityRepository{db: db}
}

// DB returns the underlying database connection.
func (r *IdentityRepository) DB() *gorm.DB {
	return r.db
}

// Create inserts a new identity binding.
func (r *IdentityRepository) Create(ctx context.Context, identity *models.UserIdentity) error {
	return r.db.WithContext(ctx).Create(identity).Error
}

// FindByProviderSubject finds an identity by provider and subject (external user ID).
func (r *IdentityRepository) FindByProviderSubject(ctx context.Context, provider, subject string) (*models.UserIdentity, error) {
	var identity models.UserIdentity
	err := r.db.WithContext(ctx).
		Where("provider = ? AND subject = ?", provider, subject).
		First(&identity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}
	return &identity, nil
}

// FindByUser finds all identities linked to a user.
func (r *IdentityRepository) FindByUser(ctx context.Context, userID uint) ([]*models.UserIdentity, error) {
	var identities []*models.UserIdentity
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&identities).Error
	return identities, err
}

// FindByUserProvider finds a user's identity for a specific provider.
func (r *IdentityRepository) FindByUserProvider(ctx context.Context, userID uint, provider string) (*models.UserIdentity, error) {
	var identity models.UserIdentity
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, provider).
		First(&identity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}
	return &identity, nil
}

// Delete removes an identity binding.
func (r *IdentityRepository) Delete(ctx context.Context, userID uint, provider string) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, provider).
		Delete(&models.UserIdentity{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

// CountByUser counts how many identities a user has.
func (r *IdentityRepository) CountByUser(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserIdentity{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// WithContext returns a copy of the repository with the given context.
func (r *IdentityRepository) WithContext(ctx context.Context) *IdentityRepository {
	return &IdentityRepository{db: r.db.WithContext(ctx)}
}
