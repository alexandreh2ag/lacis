package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChangeEventFilter selects the change events of a namespace, the latest
// first unless OldestFirst.
type ChangeEventFilter struct {
	NamespaceID uuid.UUID
	// HostID, EntityType and EntityID keep the events of a host, of a kind
	// of entity, of an entity; zero keeps all.
	HostID     *uuid.UUID
	EntityType string
	EntityID   *uuid.UUID
	// Since keeps the events from this time on; zero keeps all.
	Since time.Time
	// OldestFirst lists the events in the order they occurred.
	OldestFirst bool
	// Offset skips the first events in that order; Limit is the number of
	// events returned, zero returning all.
	Offset int
	Limit  int
}

// ChangeEventRow is a change event with the hostname of its host.
type ChangeEventRow struct {
	model.ChangeEvent `gorm:"embedded"`
	Hostname          *string
}

type ChangeEventRepository interface {
	WithTx(tx *gorm.DB) ChangeEventRepository
	Create(ctx context.Context, events []model.ChangeEvent) error
	List(ctx context.Context, filter ChangeEventFilter) ([]ChangeEventRow, error)
	// Purge deletes the events of the namespace older than its keep latest;
	// the events at the same time as the oldest kept are kept too. It
	// returns how many it deleted.
	Purge(ctx context.Context, namespaceID uuid.UUID, keep int) (int64, error)
}

type changeEventRepository struct{ db *gorm.DB }

func NewChangeEventRepository(db *gorm.DB) ChangeEventRepository {
	return &changeEventRepository{db: db}
}

func (r *changeEventRepository) WithTx(tx *gorm.DB) ChangeEventRepository {
	return &changeEventRepository{db: tx}
}

func (r *changeEventRepository) Create(ctx context.Context, events []model.ChangeEvent) error {
	if len(events) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(&events, 500).Error
}

func (r *changeEventRepository) List(ctx context.Context, filter ChangeEventFilter) ([]ChangeEventRow, error) {
	q := r.db.WithContext(ctx).Model(&model.ChangeEvent{}).
		Select("change_events.*, hosts.hostname").
		Joins("left join hosts on hosts.id = change_events.host_id").
		Where("change_events.namespace_id = ?", filter.NamespaceID)
	if filter.HostID != nil {
		q = q.Where("change_events.host_id = ?", *filter.HostID)
	}
	if filter.EntityType != "" {
		q = q.Where("change_events.entity_type = ?", filter.EntityType)
	}
	if filter.EntityID != nil {
		q = q.Where("change_events.entity_id = ?", *filter.EntityID)
	}
	if !filter.Since.IsZero() {
		q = q.Where("change_events.occurred_at >= ?", filter.Since)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	order := "change_events.occurred_at desc, change_events.id desc"
	if filter.OldestFirst {
		order = "change_events.occurred_at, change_events.id"
	}
	var rows []ChangeEventRow
	err := q.Order(order).Scan(&rows).Error
	return rows, err
}

func (r *changeEventRepository) Purge(ctx context.Context, namespaceID uuid.UUID, keep int) (int64, error) {
	db := r.db.WithContext(ctx)
	oldestKept := db.Model(&model.ChangeEvent{}).Select("occurred_at").Where("namespace_id = ?", namespaceID).
		Order("occurred_at desc").Offset(keep - 1).Limit(1)
	res := db.Where("namespace_id = ? and occurred_at < (?)", namespaceID, oldestKept).Delete(&model.ChangeEvent{})
	return res.RowsAffected, res.Error
}
