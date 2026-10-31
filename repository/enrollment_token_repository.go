package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EnrollmentTokenRepository interface {
	WithTx(tx *gorm.DB) EnrollmentTokenRepository
	Create(ctx context.Context, token *model.EnrollmentToken) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.EnrollmentToken, error)
	FindByHash(ctx context.Context, hash string) (*model.EnrollmentToken, error)
	// FindByLabel returns the first token of that label.
	FindByLabel(ctx context.Context, label string) (*model.EnrollmentToken, error)
	FindAll(ctx context.Context) ([]model.EnrollmentToken, error)
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error
	// PurgeExpired deletes the tokens expired at now that enrolled no agent,
	// and returns how many.
	PurgeExpired(ctx context.Context, now time.Time) (int64, error)
}

type enrollmentTokenRepository struct {
	db *gorm.DB
}

func NewEnrollmentTokenRepository(db *gorm.DB) EnrollmentTokenRepository {
	return &enrollmentTokenRepository{db: db}
}

func (r *enrollmentTokenRepository) WithTx(tx *gorm.DB) EnrollmentTokenRepository {
	return &enrollmentTokenRepository{db: tx}
}

func (r *enrollmentTokenRepository) Create(ctx context.Context, token *model.EnrollmentToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *enrollmentTokenRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.EnrollmentToken, error) {
	return first[model.EnrollmentToken](ctx, r.db, "id = ?", id)
}

func (r *enrollmentTokenRepository) FindByHash(ctx context.Context, hash string) (*model.EnrollmentToken, error) {
	return first[model.EnrollmentToken](ctx, r.db, "token_hash = ?", hash)
}

func (r *enrollmentTokenRepository) FindByLabel(ctx context.Context, label string) (*model.EnrollmentToken, error) {
	return first[model.EnrollmentToken](ctx, r.db, "label = ?", label)
}

func (r *enrollmentTokenRepository) FindAll(ctx context.Context) ([]model.EnrollmentToken, error) {
	var tokens []model.EnrollmentToken
	err := r.db.WithContext(ctx).Order("created_at desc, id desc").Find(&tokens).Error
	return tokens, err
}

// Revoke marks the token revoked at at; revoking it again keeps the first
// date. It returns gorm.ErrRecordNotFound when the token does not exist.
func (r *enrollmentTokenRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	return revoke[model.EnrollmentToken](ctx, r.db, id, at)
}

// revoke sets revoked_at of the row id of T, unless already set.
func revoke[T any](ctx context.Context, db *gorm.DB, id uuid.UUID, at time.Time) error {
	res := db.WithContext(ctx).Model(new(T)).
		Where("id = ?", id).
		Update("revoked_at", gorm.Expr("coalesce(revoked_at, ?)", at))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *enrollmentTokenRepository) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`delete from enrollment_tokens t
		where t.expires_at <= ? and not exists (select 1 from agents a where a.enrollment_token_id = t.id)`, now)
	return res.RowsAffected, res.Error
}
