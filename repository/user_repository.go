package repository

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserRepository interface {
	WithTx(tx *gorm.DB) UserRepository
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByOIDCSubject(ctx context.Context, subject string) (*model.User, error)
	// FindAll returns the users by username.
	FindAll(ctx context.Context) ([]model.User, error)
	// Delete removes the user, its roles and its sessions.
	Delete(ctx context.Context, id uuid.UUID) error
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) WithTx(tx *gorm.DB) UserRepository {
	return &userRepository{db: tx}
}

func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// Update saves every column of user.
func (r *userRepository) Update(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return first[model.User](ctx, r.db, "id = ?", id)
}

// FindByUsername matches the username exactly: usernames are stored
// lowercased by the services.
func (r *userRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	return first[model.User](ctx, r.db, "username = ?", username)
}

func (r *userRepository) FindByOIDCSubject(ctx context.Context, subject string) (*model.User, error) {
	return first[model.User](ctx, r.db, "oidc_subject = ?", subject)
}

func (r *userRepository) FindAll(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).Order("username").Find(&users).Error
	return users, err
}

func (r *userRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.User{}, "id = ?", id).Error
}
