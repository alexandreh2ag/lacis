// Package service holds the business logic. Services open the transactions
// and work on the repositories, never on GORM directly.
package service

import "errors"

var (
	ErrInvalid       = errors.New("invalid input")
	ErrAlreadyExists = errors.New("already exists")
	ErrNotFound      = errors.New("not found")
	// ErrUnauthorized hides why a credential is refused: unknown, expired
	// or revoked.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrLastAdmin refuses to take the admin role from the last user
	// holding it: nobody could administer Lacis any more.
	ErrLastAdmin = errors.New("the last user holding the admin role keeps it")
)
