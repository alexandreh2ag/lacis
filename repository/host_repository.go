package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type HostRepository interface {
	WithTx(tx *gorm.DB) HostRepository
	Create(ctx context.Context, host *model.Host) error
	// Update saves the columns the reception of a snapshot owns: hostname,
	// labels, agent version, hash of the last snapshot, last seen. The
	// normalization writes the others, maybe meanwhile.
	Update(ctx context.Context, host *model.Host) error
	// UpdateDescription saves the columns the normalization owns: the
	// description of the host and its project and env.
	UpdateDescription(ctx context.Context, host *model.Host) error
	// CountByHostname counts the hosts of the namespace with this
	// hostname, but the host except.
	CountByHostname(ctx context.Context, namespaceID uuid.UUID, hostname string, except uuid.UUID) (int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.Host, error)
	FindByAgentID(ctx context.Context, agentID uuid.UUID) (*model.Host, error)
	FindByHostname(ctx context.Context, namespaceID uuid.UUID, hostname string) (*model.Host, error)
	// Delete removes the host with its content and snapshots.
	Delete(ctx context.Context, id uuid.UUID) error
	// FindSeenBefore returns the hosts last seen before before, with the
	// slug of their namespace.
	FindSeenBefore(ctx context.Context, before time.Time) ([]HostRef, error)
}

// HostRef names a host and its namespace.
type HostRef struct {
	ID        uuid.UUID
	Hostname  string
	Namespace string
}

type hostRepository struct {
	db *gorm.DB
}

func NewHostRepository(db *gorm.DB) HostRepository {
	return &hostRepository{db: db}
}

func (r *hostRepository) WithTx(tx *gorm.DB) HostRepository {
	return &hostRepository{db: tx}
}

func (r *hostRepository) Create(ctx context.Context, host *model.Host) error {
	return r.db.WithContext(ctx).Create(host).Error
}

func (r *hostRepository) Update(ctx context.Context, host *model.Host) error {
	return r.db.WithContext(ctx).Model(host).
		Select("hostname", "labels", "agent_version", "last_snapshot_hash", "last_seen_at").Updates(host).Error
}

func (r *hostRepository) UpdateDescription(ctx context.Context, host *model.Host) error {
	return r.db.WithContext(ctx).Model(host).
		Select("os", "kernel", "arch", "virtualization", "resources", "project_id", "environment_id").Updates(host).Error
}

func (r *hostRepository) CountByHostname(ctx context.Context, namespaceID uuid.UUID, hostname string, except uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Host{}).
		Where("namespace_id = ? and hostname = ? and id <> ?", namespaceID, hostname, except).Count(&n).Error
	return n, err
}

func (r *hostRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.Host{}, "id = ?", id).Error
}

func (r *hostRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Host, error) {
	return first[model.Host](ctx, r.db, "id = ?", id)
}

func (r *hostRepository) FindByAgentID(ctx context.Context, agentID uuid.UUID) (*model.Host, error) {
	return first[model.Host](ctx, r.db, "agent_id = ?", agentID)
}

func (r *hostRepository) FindByHostname(ctx context.Context, namespaceID uuid.UUID, hostname string) (*model.Host, error) {
	return first[model.Host](ctx, r.db, "namespace_id = ? and hostname = ?", namespaceID, hostname)
}

func (r *hostRepository) FindSeenBefore(ctx context.Context, before time.Time) ([]HostRef, error) {
	var refs []HostRef
	err := r.db.WithContext(ctx).Table("hosts").
		Select("hosts.id, hosts.hostname, namespaces.slug as namespace").
		Joins("join namespaces on namespaces.id = hosts.namespace_id").
		Where("hosts.last_seen_at < ?", before).Order("namespaces.slug, hosts.hostname").Scan(&refs).Error
	return refs, err
}
