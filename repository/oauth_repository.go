package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OAuthPurged counts the rows a purge of the OAuth tables deleted.
type OAuthPurged struct {
	Codes   int64
	Grants  int64
	Clients int64
}

type OAuthRepository interface {
	WithTx(tx *gorm.DB) OAuthRepository
	CreateClient(ctx context.Context, client *model.OAuthClient) error
	FindClient(ctx context.Context, id string) (*model.OAuthClient, error)
	// MarkClientAuthorized records the first authorization of the client.
	MarkClientAuthorized(ctx context.Context, id string, at time.Time) error
	CreateCode(ctx context.Context, code *model.OAuthCode) error
	// TakeCode deletes the code of this id and returns it, unless it
	// expired at now: a code serves once.
	TakeCode(ctx context.Context, id string, now time.Time) (*model.OAuthCode, error)
	CreateGrant(ctx context.Context, grant *model.OAuthGrant) error
	// FindGrantByAccess returns the grant of an access token hash, unless
	// the token or the grant expired at now.
	FindGrantByAccess(ctx context.Context, accessHash string, now time.Time) (*model.OAuthGrant, error)
	// FindGrantByRefresh returns the grant whose current refresh token, or
	// else whose previous one, has this hash; previous tells which.
	FindGrantByRefresh(ctx context.Context, refreshHash string) (grant *model.OAuthGrant, previous bool, err error)
	// SaveGrant updates the tokens and the last use of the grant.
	SaveGrant(ctx context.Context, grant *model.OAuthGrant) error
	// TouchGrant records a use of the grant.
	TouchGrant(ctx context.Context, id uuid.UUID, at time.Time) error
	DeleteGrant(ctx context.Context, id uuid.UUID) error
	// DeleteUserGrant deletes the grant of the user; false when the user
	// has none of this id.
	DeleteUserGrant(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// FindUserGrants returns the grants of the user not expired at now,
	// latest first, with the name of their client.
	FindUserGrants(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.OAuthGrantView, error)
	// Purge deletes the codes and grants expired at now, and the clients
	// registered before unauthorizedBefore that no user ever authorized.
	Purge(ctx context.Context, now, unauthorizedBefore time.Time) (OAuthPurged, error)
}

type oauthRepository struct{ db *gorm.DB }

func NewOAuthRepository(db *gorm.DB) OAuthRepository { return &oauthRepository{db: db} }

func (r *oauthRepository) WithTx(tx *gorm.DB) OAuthRepository { return &oauthRepository{db: tx} }

func (r *oauthRepository) CreateClient(ctx context.Context, client *model.OAuthClient) error {
	return r.db.WithContext(ctx).Create(client).Error
}

func (r *oauthRepository) FindClient(ctx context.Context, id string) (*model.OAuthClient, error) {
	return first[model.OAuthClient](ctx, r.db, "id = ?", id)
}

func (r *oauthRepository) MarkClientAuthorized(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.OAuthClient{}).Where("id = ? and authorized_at is null", id).
		Update("authorized_at", at).Error
}

func (r *oauthRepository) CreateCode(ctx context.Context, code *model.OAuthCode) error {
	return r.db.WithContext(ctx).Create(code).Error
}

func (r *oauthRepository) TakeCode(ctx context.Context, id string, now time.Time) (*model.OAuthCode, error) {
	var codes []model.OAuthCode
	// Deleted whether expired or not: an expired code serves no more.
	if err := r.db.WithContext(ctx).Clauses(clause.Returning{}).Where("id = ?", id).Delete(&codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 || !now.Before(codes[0].ExpiresAt) {
		return nil, gorm.ErrRecordNotFound
	}
	return &codes[0], nil
}

func (r *oauthRepository) CreateGrant(ctx context.Context, grant *model.OAuthGrant) error {
	return r.db.WithContext(ctx).Create(grant).Error
}

func (r *oauthRepository) FindGrantByAccess(ctx context.Context, accessHash string, now time.Time) (*model.OAuthGrant, error) {
	return first[model.OAuthGrant](ctx, r.db, "access_hash = ? and access_expires_at > ? and expires_at > ?", accessHash, now, now)
}

func (r *oauthRepository) FindGrantByRefresh(ctx context.Context, refreshHash string) (*model.OAuthGrant, bool, error) {
	grant, err := first[model.OAuthGrant](ctx, r.db.Clauses(clause.Locking{Strength: "UPDATE"}),
		"refresh_hash = ? or previous_refresh_hash = ?", refreshHash, refreshHash)
	if err != nil {
		return nil, false, err
	}
	return grant, grant.RefreshHash != refreshHash, nil
}

func (r *oauthRepository) SaveGrant(ctx context.Context, grant *model.OAuthGrant) error {
	return r.db.WithContext(ctx).Model(&model.OAuthGrant{}).Where("id = ?", grant.ID).Updates(map[string]any{
		"access_hash": grant.AccessHash, "access_expires_at": grant.AccessExpiresAt, "refresh_hash": grant.RefreshHash,
		"previous_refresh_hash": grant.PreviousRefreshHash, "last_used_at": grant.LastUsedAt,
	}).Error
}

func (r *oauthRepository) TouchGrant(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.OAuthGrant{}).Where("id = ?", id).Update("last_used_at", at).Error
}

func (r *oauthRepository) DeleteGrant(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.OAuthGrant{}, "id = ?", id).Error
}

func (r *oauthRepository) DeleteUserGrant(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Delete(&model.OAuthGrant{}, "id = ? and user_id = ?", id, userID)
	return res.RowsAffected > 0, res.Error
}

func (r *oauthRepository) FindUserGrants(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.OAuthGrantView, error) {
	var rows []model.OAuthGrantView
	err := r.db.WithContext(ctx).Table("oauth_grants g").Select("g.*, c.name as client_name").
		Joins("join oauth_clients c on c.id = g.client_id").
		Where("g.user_id = ? and g.expires_at > ?", userID, now).Order("g.created_at desc, g.id").Scan(&rows).Error
	return rows, err
}

func (r *oauthRepository) Purge(ctx context.Context, now, unauthorizedBefore time.Time) (OAuthPurged, error) {
	var out OAuthPurged
	db := r.db.WithContext(ctx)
	res := db.Delete(&model.OAuthCode{}, "expires_at <= ?", now)
	if res.Error != nil {
		return out, res.Error
	}
	out.Codes = res.RowsAffected
	if res = db.Delete(&model.OAuthGrant{}, "expires_at <= ?", now); res.Error != nil {
		return out, res.Error
	}
	out.Grants = res.RowsAffected
	if res = db.Delete(&model.OAuthClient{}, "authorized_at is null and created_at <= ?", unauthorizedBefore); res.Error != nil {
		return out, res.Error
	}
	out.Clients = res.RowsAffected
	return out, nil
}
