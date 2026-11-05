package repository

import (
	"context"
	"fmt"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Usage counts the hosts and the apps not removed of the project.
	Usage(ctx context.Context, id uuid.UUID) (Usage, error)
	// Delete removes the project, after taking it from the removed apps;
	// the caller checks first that Usage is zero.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteUnused removes, in every namespace, the projects of no host
	// and of no app but removed ones, and returns them.
	DeleteUnused(ctx context.Context) ([]model.Project, error)
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
	// Usage counts the hosts and the apps not removed of the env.
	Usage(ctx context.Context, id uuid.UUID) (Usage, error)
	// Delete removes the env, after taking it from the removed apps; the
	// caller checks first that Usage is zero.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteUnused removes, in every namespace, the envs of no host and of
	// no app but removed ones, and returns them.
	DeleteUnused(ctx context.Context) ([]model.Environment, error)
}

// Usage is what still names a project or an env: a removed app does not
// count, it loses the reference when the project or env goes.
type Usage struct {
	Hosts int64
	Apps  int64
}

// Unused reports that nothing names the project or env any more.
func (u Usage) Unused() bool { return u.Hosts == 0 && u.Apps == 0 }

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

func (r *projectRepository) Usage(ctx context.Context, id uuid.UUID) (Usage, error) {
	return catalogUsage(ctx, r.db, "project_id", id)
}

func (r *projectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return deleteCatalog[model.Project](ctx, r.db, "project_id", id)
}

func (r *projectRepository) DeleteUnused(ctx context.Context) ([]model.Project, error) {
	return deleteUnusedCatalogs[model.Project](ctx, r.db, "projects", "project_id")
}

func (r *environmentRepository) Usage(ctx context.Context, id uuid.UUID) (Usage, error) {
	return catalogUsage(ctx, r.db, "environment_id", id)
}

func (r *environmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return deleteCatalog[model.Environment](ctx, r.db, "environment_id", id)
}

func (r *environmentRepository) DeleteUnused(ctx context.Context) ([]model.Environment, error) {
	return deleteUnusedCatalogs[model.Environment](ctx, r.db, "environments", "environment_id")
}

// column is project_id or environment_id, the column of hosts and apps
// naming the catalog row, and table the catalog: never a user input.
func catalogUsage(ctx context.Context, db *gorm.DB, column string, id uuid.UUID) (Usage, error) {
	var u Usage
	db = db.WithContext(ctx)
	if err := db.Model(&model.Host{}).Where(column+" = ?", id).Count(&u.Hosts).Error; err != nil {
		return u, err
	}
	err := db.Model(&model.App{}).Where(column+" = ? and removed_at is null", id).Count(&u.Apps).Error
	return u, err
}

func deleteCatalog[T any](ctx context.Context, db *gorm.DB, column string, id uuid.UUID) error {
	db = db.WithContext(ctx)
	if err := db.Model(&model.App{}).Where(column+" = ? and removed_at is not null", id).Update(column, nil).Error; err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(new(T)).Error
}

// unusedCatalog selects the rows of no host and of no app but removed ones.
const unusedCatalog = "not exists (select 1 from hosts h where h.%[1]s = %[2]s.id)" +
	" and not exists (select 1 from apps a where a.%[1]s = %[2]s.id and a.removed_at is null)"

func deleteUnusedCatalogs[T any](ctx context.Context, db *gorm.DB, table, column string) ([]T, error) {
	db = db.WithContext(ctx)
	var ids []uuid.UUID
	if err := db.Model(new(T)).Where(fmt.Sprintf(unusedCatalog, column, table)).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return nil, err
	}
	if err := db.Model(&model.App{}).Where(column+" in ? and removed_at is not null", ids).Update(column, nil).Error; err != nil {
		return nil, err
	}
	// The condition again: a snapshot may have used one meanwhile.
	var deleted []T
	err := db.Clauses(clause.Returning{}).Where("id in ?", ids).Where(fmt.Sprintf(unusedCatalog, column, table)).Delete(&deleted).Error
	return deleted, err
}
