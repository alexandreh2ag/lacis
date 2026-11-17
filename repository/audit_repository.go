package repository

import (
	"context"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"gorm.io/gorm"
)

// The columns the audit events sort by.
const (
	AuditSortAt        = "at"
	AuditSortUsername  = "username"
	AuditSortSource    = "source"
	AuditSortAction    = "action"
	AuditSortNamespace = "namespace"
	AuditSortTarget    = "target"
)

// auditSorts are the orders of the columns, the newest event first among
// equals.
var auditSorts = map[string]string{
	AuditSortAt: "at", AuditSortUsername: "lower(username)", AuditSortSource: "source", AuditSortAction: "lower(action)",
	AuditSortNamespace: "namespace", AuditSortTarget: "lower(target)",
}

// AuditFilter selects audit events, the latest first unless Sort says
// otherwise.
type AuditFilter struct {
	// From and To keep the events from From (included) to To (excluded);
	// zero bounds nothing.
	From, To time.Time
	// Username keeps the events of the users whose username contains it,
	// case-insensitive.
	Username string
	// Action keeps the events whose action contains it, case-insensitive.
	Action string
	// Sources keeps the events of these sources; empty keeps all.
	Sources []model.AuditSource
	// Search keeps the events whose action, namespace, project, target or
	// details contain it, case-insensitive.
	Search string
	// Sort is the column to sort by, ascending unless Desc; empty or
	// unknown, the newest event comes first.
	Sort   string
	Desc   bool
	Offset int
	Limit  int
}

type AuditRepository interface {
	WithTx(tx *gorm.DB) AuditRepository
	Create(ctx context.Context, event *model.AuditEvent) error
	// List returns a page of the events of the filter and how many match.
	List(ctx context.Context, filter AuditFilter) ([]model.AuditEvent, int64, error)
	// Usernames returns the usernames of the events containing search, in
	// alphabetical order, limit at most.
	Usernames(ctx context.Context, search string, limit int) ([]string, error)
	// Actions returns the actions of the events containing search, in
	// alphabetical order, limit at most.
	Actions(ctx context.Context, search string, limit int) ([]string, error)
	// PurgeBefore deletes the events older than before and returns how
	// many.
	PurgeBefore(ctx context.Context, before time.Time) (int64, error)
}

type auditRepository struct{ db *gorm.DB }

func NewAuditRepository(db *gorm.DB) AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) WithTx(tx *gorm.DB) AuditRepository {
	return &auditRepository{db: tx}
}

func (r *auditRepository) Create(ctx context.Context, event *model.AuditEvent) error {
	if event.Details == nil {
		event.Details = []byte("{}")
	}
	return r.db.WithContext(ctx).Create(event).Error
}

// like escapes s for a LIKE pattern containing it.
func like(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s) + "%"
}

func (r *auditRepository) List(ctx context.Context, filter AuditFilter) ([]model.AuditEvent, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AuditEvent{})
	if !filter.From.IsZero() {
		q = q.Where("at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		q = q.Where("at < ?", filter.To)
	}
	if filter.Username != "" {
		q = q.Where("username ilike ?", like(filter.Username))
	}
	if filter.Action != "" {
		q = q.Where("action ilike ?", like(filter.Action))
	}
	if len(filter.Sources) > 0 {
		q = q.Where("source in ?", filter.Sources)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		p := like(s)
		q = q.Where("action ilike ? or namespace ilike ? or project ilike ? or target ilike ? or details::text ilike ?", p, p, p, p, p)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	var events []model.AuditEvent
	column, ok := auditSorts[filter.Sort]
	dir := "asc"
	if filter.Desc || !ok {
		dir = "desc"
	}
	if !ok {
		column = auditSorts[AuditSortAt]
	}
	// Empty columns go last whatever the direction; ties keep the newest first.
	err := q.Order(column + " " + dir + " nulls last, at desc, id desc").Find(&events).Error
	return events, total, err
}

func (r *auditRepository) Usernames(ctx context.Context, search string, limit int) ([]string, error) {
	return r.distinct(ctx, "username", search, limit)
}

func (r *auditRepository) Actions(ctx context.Context, search string, limit int) ([]string, error) {
	return r.distinct(ctx, "action", search, limit)
}

// distinct returns the values of column containing search, each once.
func (r *auditRepository) distinct(ctx context.Context, column, search string, limit int) ([]string, error) {
	q := r.db.WithContext(ctx).Model(&model.AuditEvent{}).Distinct(column).Order(column)
	if s := strings.TrimSpace(search); s != "" {
		q = q.Where(column+" ilike ?", like(s))
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var values []string
	err := q.Pluck(column, &values).Error
	return values, err
}

func (r *auditRepository) PurgeBefore(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("at < ?", before).Delete(&model.AuditEvent{})
	return res.RowsAffected, res.Error
}
