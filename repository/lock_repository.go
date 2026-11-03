package repository

import (
	"context"

	"gorm.io/gorm"
)

// LockRepository serializes the work on a resource across the workers.
type LockRepository interface {
	WithTx(tx *gorm.DB) LockRepository
	// Lock waits for the lock of key and holds it until the transaction
	// ends: a Postgres advisory lock, which blocks no row, so the requests
	// reading or touching the rows meanwhile are not delayed. It must run
	// in a transaction.
	Lock(ctx context.Context, key string) error
}

type lockRepository struct{ db *gorm.DB }

func NewLockRepository(db *gorm.DB) LockRepository { return &lockRepository{db: db} }

func (r *lockRepository) WithTx(tx *gorm.DB) LockRepository { return &lockRepository{db: tx} }

func (r *lockRepository) Lock(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Exec("select pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error
}
