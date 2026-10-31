package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The content of a host is synchronized with each snapshot: a row is found
// by its natural key, saved, and the rows missing from the snapshot get
// removed_at.

type HostIPRepository interface {
	WithTx(tx *gorm.DB) HostIPRepository
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.HostIP, error)
	// Replace makes ips the IPs of the host.
	Replace(ctx context.Context, hostID uuid.UUID, ips []model.HostIP) error
}

type RuntimeRepository interface {
	WithTx(tx *gorm.DB) RuntimeRepository
	// FindByHost returns every runtime of the host, removed ones included.
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Runtime, error)
	Save(ctx context.Context, runtime *model.Runtime) error
	// MarkRemoved sets removed_at on the runtimes of the host but keep.
	MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error
}

type ServiceRepository interface {
	WithTx(tx *gorm.DB) ServiceRepository
	// FindByHost returns every service of the host, removed ones included.
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Service, error)
	Save(ctx context.Context, service *model.Service) error
	// MarkRemoved sets removed_at on the services of the host but keep.
	MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error
}

type EndpointRepository interface {
	WithTx(tx *gorm.DB) EndpointRepository
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Endpoint, error)
	// Replace makes endpoints the endpoints of the host owned by ownerType.
	Replace(ctx context.Context, hostID uuid.UUID, ownerType string, endpoints []model.Endpoint) error
}

func findByHost[T any](ctx context.Context, db *gorm.DB, hostID uuid.UUID) ([]T, error) {
	var rows []T
	err := db.WithContext(ctx).Where("host_id = ?", hostID).Find(&rows).Error
	return rows, err
}

func markRemoved[T any](ctx context.Context, db *gorm.DB, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error {
	q := db.WithContext(ctx).Model(new(T)).Where("host_id = ? and removed_at is null", hostID)
	if len(keep) > 0 {
		q = q.Where("id not in ?", keep)
	}
	return q.Update("removed_at", at).Error
}

type hostIPRepository struct{ db *gorm.DB }

func NewHostIPRepository(db *gorm.DB) HostIPRepository { return &hostIPRepository{db: db} }

func (r *hostIPRepository) WithTx(tx *gorm.DB) HostIPRepository { return &hostIPRepository{db: tx} }

func (r *hostIPRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.HostIP, error) {
	var ips []model.HostIP
	err := r.db.WithContext(ctx).Where("host_id = ?", hostID).Order("ip").Find(&ips).Error
	return ips, err
}

func (r *hostIPRepository) Replace(ctx context.Context, hostID uuid.UUID, ips []model.HostIP) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("host_id = ?", hostID).Delete(&model.HostIP{}).Error; err != nil {
		return err
	}
	if len(ips) == 0 {
		return nil
	}
	return db.Create(&ips).Error
}

type runtimeRepository struct{ db *gorm.DB }

func NewRuntimeRepository(db *gorm.DB) RuntimeRepository { return &runtimeRepository{db: db} }

func (r *runtimeRepository) WithTx(tx *gorm.DB) RuntimeRepository { return &runtimeRepository{db: tx} }

func (r *runtimeRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Runtime, error) {
	return findByHost[model.Runtime](ctx, r.db, hostID)
}

func (r *runtimeRepository) Save(ctx context.Context, runtime *model.Runtime) error {
	return r.db.WithContext(ctx).Save(runtime).Error
}

func (r *runtimeRepository) MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error {
	return markRemoved[model.Runtime](ctx, r.db, hostID, keep, at)
}

type serviceRepository struct{ db *gorm.DB }

func NewServiceRepository(db *gorm.DB) ServiceRepository { return &serviceRepository{db: db} }

func (r *serviceRepository) WithTx(tx *gorm.DB) ServiceRepository { return &serviceRepository{db: tx} }

func (r *serviceRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Service, error) {
	return findByHost[model.Service](ctx, r.db, hostID)
}

func (r *serviceRepository) Save(ctx context.Context, service *model.Service) error {
	return r.db.WithContext(ctx).Save(service).Error
}

func (r *serviceRepository) MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error {
	return markRemoved[model.Service](ctx, r.db, hostID, keep, at)
}

type endpointRepository struct{ db *gorm.DB }

func NewEndpointRepository(db *gorm.DB) EndpointRepository { return &endpointRepository{db: db} }

func (r *endpointRepository) WithTx(tx *gorm.DB) EndpointRepository {
	return &endpointRepository{db: tx}
}

func (r *endpointRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Endpoint, error) {
	var endpoints []model.Endpoint
	err := r.db.WithContext(ctx).Where("host_id = ?", hostID).Order("kind, address, port").Find(&endpoints).Error
	return endpoints, err
}

func (r *endpointRepository) Replace(ctx context.Context, hostID uuid.UUID, ownerType string, endpoints []model.Endpoint) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("host_id = ? and owner_type = ?", hostID, ownerType).Delete(&model.Endpoint{}).Error; err != nil {
		return err
	}
	if len(endpoints) == 0 {
		return nil
	}
	return db.Create(&endpoints).Error
}

type WebserverRepository interface {
	WithTx(tx *gorm.DB) WebserverRepository
	// FindByHost returns every webserver of the host, removed ones included.
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Webserver, error)
	Save(ctx context.Context, webserver *model.Webserver) error
	// MarkRemoved sets removed_at on the webservers of the host but keep.
	MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error
}

type AppRepository interface {
	WithTx(tx *gorm.DB) AppRepository
	// FindByHost returns every app of the host, removed ones included.
	FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.App, error)
	Save(ctx context.Context, app *model.App) error
	// MarkRemoved sets removed_at on the apps of the host but keep.
	MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error
}

type AppDependencyRepository interface {
	WithTx(tx *gorm.DB) AppDependencyRepository
	FindByApp(ctx context.Context, appID uuid.UUID) ([]model.AppDependency, error)
	// Replace makes deps the dependencies of the app.
	Replace(ctx context.Context, appID uuid.UUID, deps []model.AppDependency) error
}

type webserverRepository struct{ db *gorm.DB }

func NewWebserverRepository(db *gorm.DB) WebserverRepository { return &webserverRepository{db: db} }

func (r *webserverRepository) WithTx(tx *gorm.DB) WebserverRepository {
	return &webserverRepository{db: tx}
}

func (r *webserverRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.Webserver, error) {
	return findByHost[model.Webserver](ctx, r.db, hostID)
}

func (r *webserverRepository) Save(ctx context.Context, webserver *model.Webserver) error {
	return r.db.WithContext(ctx).Save(webserver).Error
}

func (r *webserverRepository) MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error {
	return markRemoved[model.Webserver](ctx, r.db, hostID, keep, at)
}

type appRepository struct{ db *gorm.DB }

func NewAppRepository(db *gorm.DB) AppRepository { return &appRepository{db: db} }

func (r *appRepository) WithTx(tx *gorm.DB) AppRepository { return &appRepository{db: tx} }

func (r *appRepository) FindByHost(ctx context.Context, hostID uuid.UUID) ([]model.App, error) {
	return findByHost[model.App](ctx, r.db, hostID)
}

func (r *appRepository) Save(ctx context.Context, app *model.App) error {
	return r.db.WithContext(ctx).Save(app).Error
}

func (r *appRepository) MarkRemoved(ctx context.Context, hostID uuid.UUID, keep []uuid.UUID, at time.Time) error {
	return markRemoved[model.App](ctx, r.db, hostID, keep, at)
}

type appDependencyRepository struct{ db *gorm.DB }

func NewAppDependencyRepository(db *gorm.DB) AppDependencyRepository {
	return &appDependencyRepository{db: db}
}

func (r *appDependencyRepository) WithTx(tx *gorm.DB) AppDependencyRepository {
	return &appDependencyRepository{db: tx}
}

func (r *appDependencyRepository) FindByApp(ctx context.Context, appID uuid.UUID) ([]model.AppDependency, error) {
	var deps []model.AppDependency
	err := r.db.WithContext(ctx).Where("app_id = ?", appID).Order("key, id").Find(&deps).Error
	return deps, err
}

func (r *appDependencyRepository) Replace(ctx context.Context, appID uuid.UUID, deps []model.AppDependency) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("app_id = ?", appID).Delete(&model.AppDependency{}).Error; err != nil {
		return err
	}
	if len(deps) == 0 {
		return nil
	}
	return db.Create(&deps).Error
}
