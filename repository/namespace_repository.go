package repository

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NamespaceRepository interface {
	WithTx(tx *gorm.DB) NamespaceRepository
	Create(ctx context.Context, namespace *model.Namespace) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.Namespace, error)
	FindBySlug(ctx context.Context, slug string) (*model.Namespace, error)
	FindAll(ctx context.Context) ([]model.Namespace, error)
	// Update saves every column of namespace.
	Update(ctx context.Context, namespace *model.Namespace) error
}

type namespaceRepository struct {
	db *gorm.DB
}

func NewNamespaceRepository(db *gorm.DB) NamespaceRepository {
	return &namespaceRepository{db: db}
}

func (r *namespaceRepository) WithTx(tx *gorm.DB) NamespaceRepository {
	return &namespaceRepository{db: tx}
}

func (r *namespaceRepository) Create(ctx context.Context, namespace *model.Namespace) error {
	return r.db.WithContext(ctx).Create(namespace).Error
}

func (r *namespaceRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Namespace, error) {
	return first[model.Namespace](ctx, r.db, "id = ?", id)
}

func (r *namespaceRepository) FindBySlug(ctx context.Context, slug string) (*model.Namespace, error) {
	return first[model.Namespace](ctx, r.db, "slug = ?", slug)
}

func (r *namespaceRepository) Update(ctx context.Context, namespace *model.Namespace) error {
	return r.db.WithContext(ctx).Save(namespace).Error
}

func (r *namespaceRepository) FindAll(ctx context.Context) ([]model.Namespace, error) {
	var namespaces []model.Namespace
	err := r.db.WithContext(ctx).Order("slug").Find(&namespaces).Error
	return namespaces, err
}

// first returns the first row matching the condition, or
// gorm.ErrRecordNotFound.
func first[T any](ctx context.Context, db *gorm.DB, query string, args ...any) (*T, error) {
	var row T
	if err := db.WithContext(ctx).Where(query, args...).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
