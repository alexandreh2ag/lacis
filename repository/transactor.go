package repository

import (
	"context"

	"gorm.io/gorm"
)

// Transactor runs a function in a database transaction, with repositories
// bound to it. Services use it rather than *gorm.DB so that tests can run
// the function on mocked repositories.
type Transactor interface {
	// InTx commits when fn returns nil and rolls back otherwise.
	InTx(ctx context.Context, fn func(repos *Repositories) error) error
}

type transactor struct {
	db *gorm.DB
}

func NewTransactor(db *gorm.DB) Transactor {
	return &transactor{db: db}
}

func (t *transactor) InTx(ctx context.Context, fn func(repos *Repositories) error) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewRepositories(tx))
	})
}
