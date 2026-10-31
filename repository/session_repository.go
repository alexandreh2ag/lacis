package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SessionRepository interface {
	WithTx(tx *gorm.DB) SessionRepository
	Create(ctx context.Context, session *model.Session) error
	// FindValid returns the session of this id, unless it expired at now.
	FindValid(ctx context.Context, id string, now time.Time) (*model.Session, error)
	// Touch records an access to the session and pushes back its
	// expiration.
	Touch(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error
	Delete(ctx context.Context, id string) error
	// DeleteByUser deletes the sessions of the user, but the one of id keep.
	DeleteByUser(ctx context.Context, userID uuid.UUID, keep string) error
	// DeleteExpired deletes the sessions expired at now; it returns how
	// many it deleted.
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

type sessionRepository struct{ db *gorm.DB }

func NewSessionRepository(db *gorm.DB) SessionRepository { return &sessionRepository{db: db} }

func (r *sessionRepository) WithTx(tx *gorm.DB) SessionRepository { return &sessionRepository{db: tx} }

func (r *sessionRepository) Create(ctx context.Context, session *model.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *sessionRepository) FindValid(ctx context.Context, id string, now time.Time) (*model.Session, error) {
	return first[model.Session](ctx, r.db, "id = ? and expires_at > ?", id, now)
}

func (r *sessionRepository) Touch(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", id).
		Updates(map[string]any{"last_seen_at": lastSeenAt, "expires_at": expiresAt}).Error
}

func (r *sessionRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.Session{}, "id = ?", id).Error
}

func (r *sessionRepository) DeleteByUser(ctx context.Context, userID uuid.UUID, keep string) error {
	return r.db.WithContext(ctx).Delete(&model.Session{}, "user_id = ? and id <> ?", userID, keep).Error
}

func (r *sessionRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Delete(&model.Session{}, "expires_at <= ?", now)
	return res.RowsAffected, res.Error
}
