package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SnapshotRepository interface {
	WithTx(tx *gorm.DB) SnapshotRepository
	Create(ctx context.Context, snapshot *model.Snapshot) error
	FindByID(ctx context.Context, id int64) (*model.Snapshot, error)
	FindLatestByHost(ctx context.Context, hostID uuid.UUID) (*model.Snapshot, error)
	// FindLatestIDs returns the id of the latest snapshot of each host.
	FindLatestIDs(ctx context.Context) ([]int64, error)
	// PurgeOutdated deletes the snapshots received before now less the
	// snapshot retention of their namespace, but the latest of each host,
	// and returns how many it deleted.
	PurgeOutdated(ctx context.Context, now time.Time) (int64, error)
	// FindHostnames returns, by agent, the hostnames of the snapshots
	// received since since, oldest first.
	FindHostnames(ctx context.Context, since time.Time) (map[uuid.UUID][]string, error)
}

type snapshotRepository struct {
	db *gorm.DB
}

func NewSnapshotRepository(db *gorm.DB) SnapshotRepository {
	return &snapshotRepository{db: db}
}

func (r *snapshotRepository) WithTx(tx *gorm.DB) SnapshotRepository {
	return &snapshotRepository{db: tx}
}

func (r *snapshotRepository) Create(ctx context.Context, snapshot *model.Snapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *snapshotRepository) FindByID(ctx context.Context, id int64) (*model.Snapshot, error) {
	return first[model.Snapshot](ctx, r.db, "id = ?", id)
}

// FindLatestByHost returns the snapshot of the host collected last.
func (r *snapshotRepository) FindLatestByHost(ctx context.Context, hostID uuid.UUID) (*model.Snapshot, error) {
	var snapshot model.Snapshot
	err := r.db.WithContext(ctx).Where("host_id = ?", hostID).
		Order("collected_at desc, id desc").First(&snapshot).Error
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *snapshotRepository) FindLatestIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Raw(`select id from (
		select distinct on (host_id) id from snapshots order by host_id, collected_at desc, id desc
	) latest order by id`).Scan(&ids).Error
	return ids, err
}

func (r *snapshotRepository) PurgeOutdated(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`delete from snapshots s
		using hosts h, namespaces n
		where s.host_id = h.id and h.namespace_id = n.id
		  and s.received_at < ?::timestamptz - n.snapshot_retention
		  and s.id <> (select l.id from snapshots l where l.host_id = s.host_id order by l.collected_at desc, l.id desc limit 1)`, now)
	return res.RowsAffected, res.Error
}

func (r *snapshotRepository) FindHostnames(ctx context.Context, since time.Time) (map[uuid.UUID][]string, error) {
	var rows []struct {
		AgentID  uuid.UUID
		Hostname string
	}
	err := r.db.WithContext(ctx).Raw(`select h.agent_id, s.payload->'host'->>'hostname' as hostname
		from snapshots s join hosts h on h.id = s.host_id
		where s.received_at >= ?
		order by h.agent_id, s.received_at, s.id`, since).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]string{}
	for _, row := range rows {
		out[row.AgentID] = append(out[row.AgentID], row.Hostname)
	}
	return out, nil
}
