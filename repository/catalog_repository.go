package repository

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProjectRepository and EnvironmentRepository hold the catalogs of a
// namespace, filled from the labels on first sight.
type ProjectRepository interface {
	WithTx(tx *gorm.DB) ProjectRepository
	// Ensure returns the project of that slug, created when missing, with
	// its slug as name.
	Ensure(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Project, error)
	FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Project, error)
	// Update saves every column of project.
	Update(ctx context.Context, project *model.Project) error
}

type EnvironmentRepository interface {
	WithTx(tx *gorm.DB) EnvironmentRepository
	// Ensure returns the environment of that slug, created when missing,
	// with its slug as name.
	Ensure(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Environment, error)
	FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Environment, error)
	// FindAll returns the envs of the namespace in their order.
	FindAll(ctx context.Context, namespaceID uuid.UUID) ([]model.Environment, error)
	// Update saves every column of env.
	Update(ctx context.Context, env *model.Environment) error
}

type projectRepository struct {
	db *gorm.DB
}

func NewProjectRepository(db *gorm.DB) ProjectRepository {
	return &projectRepository{db: db}
}

func (r *projectRepository) WithTx(tx *gorm.DB) ProjectRepository {
	return &projectRepository{db: tx}
}

func (r *projectRepository) Ensure(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Project, error) {
	p := &model.Project{NamespaceID: namespaceID, Slug: slug, Name: slug}
	if err := ensure(ctx, r.db, p, "namespace_id = ? and slug = ?", namespaceID, slug); err != nil {
		return nil, err
	}
	return p, nil
}

type environmentRepository struct {
	db *gorm.DB
}

func NewEnvironmentRepository(db *gorm.DB) EnvironmentRepository {
	return &environmentRepository{db: db}
}

func (r *environmentRepository) WithTx(tx *gorm.DB) EnvironmentRepository {
	return &environmentRepository{db: tx}
}

func (r *environmentRepository) Ensure(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Environment, error) {
	e := &model.Environment{NamespaceID: namespaceID, Slug: slug, Name: slug}
	if err := ensure(ctx, r.db, e, "namespace_id = ? and slug = ?", namespaceID, slug); err != nil {
		return nil, err
	}
	return e, nil
}

func (r *projectRepository) FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Project, error) {
	return first[model.Project](ctx, r.db, "namespace_id = ? and slug = ?", namespaceID, slug)
}

func (r *projectRepository) Update(ctx context.Context, project *model.Project) error {
	return r.db.WithContext(ctx).Save(project).Error
}

func (r *environmentRepository) FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Environment, error) {
	return first[model.Environment](ctx, r.db, "namespace_id = ? and slug = ?", namespaceID, slug)
}

func (r *environmentRepository) FindAll(ctx context.Context, namespaceID uuid.UUID) ([]model.Environment, error) {
	var envs []model.Environment
	err := r.db.WithContext(ctx).Where("namespace_id = ?", namespaceID).Order("sort_order, slug").Find(&envs).Error
	return envs, err
}

func (r *environmentRepository) Update(ctx context.Context, env *model.Environment) error {
	return r.db.WithContext(ctx).Save(env).Error
}
