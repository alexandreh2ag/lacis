package repository

import (
	"context"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentRepository interface {
	WithTx(tx *gorm.DB) AgentRepository
	Create(ctx context.Context, agent *model.Agent) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.Agent, error)
	FindByTokenHash(ctx context.Context, hash string) (*model.Agent, error)
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error
	// Touch sets last_seen_at.
	Touch(ctx context.Context, id uuid.UUID, at time.Time) error
	// FindAll returns the agents with their host and enrollment token, the
	// last enrolled first.
	FindAll(ctx context.Context) ([]AgentRow, error)
	// CountByEnrollmentToken counts the agents enrolled by each token.
	CountByEnrollmentToken(ctx context.Context) (map[uuid.UUID]int, error)
	// SetConflicts marks the agents of conflicts as shared by these
	// hostnames, since at unless marked already, and clears the others.
	SetConflicts(ctx context.Context, conflicts map[uuid.UUID][]string, at time.Time) error
}

// AgentRow is an agent, the host of its snapshots and the label of the
// token that enrolled it. Host fields are nil before the first snapshot.
type AgentRow struct {
	model.Agent
	HostID     *uuid.UUID
	Hostname   *string
	Namespace  *string
	TokenLabel *string
}

type agentRepository struct {
	db *gorm.DB
}

func NewAgentRepository(db *gorm.DB) AgentRepository {
	return &agentRepository{db: db}
}

func (r *agentRepository) WithTx(tx *gorm.DB) AgentRepository {
	return &agentRepository{db: tx}
}

func (r *agentRepository) Create(ctx context.Context, agent *model.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
	return first[model.Agent](ctx, r.db, "id = ?", id)
}

func (r *agentRepository) FindByTokenHash(ctx context.Context, hash string) (*model.Agent, error) {
	return first[model.Agent](ctx, r.db, "token_hash = ?", hash)
}

// Revoke marks the agent revoked at at; revoking it again keeps the first
// date. It returns gorm.ErrRecordNotFound when the agent does not exist.
func (r *agentRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	return revoke[model.Agent](ctx, r.db, id, at)
}

func (r *agentRepository) Touch(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", id).Update("last_seen_at", at).Error
}

func (r *agentRepository) FindAll(ctx context.Context) ([]AgentRow, error) {
	var rows []AgentRow
	err := r.db.WithContext(ctx).Table("agents").
		Select("agents.*, hosts.id as host_id, hosts.hostname, namespaces.slug as namespace, enrollment_tokens.label as token_label").
		Joins("left join hosts on hosts.agent_id = agents.id").
		Joins("left join namespaces on namespaces.id = hosts.namespace_id").
		Joins("left join enrollment_tokens on enrollment_tokens.id = agents.enrollment_token_id").
		Order("agents.created_at desc, agents.id desc").
		Scan(&rows).Error
	return rows, err
}

func (r *agentRepository) CountByEnrollmentToken(ctx context.Context) (map[uuid.UUID]int, error) {
	var rows []struct {
		EnrollmentTokenID uuid.UUID
		Count             int
	}
	err := r.db.WithContext(ctx).Model(&model.Agent{}).
		Select("enrollment_token_id, count(*) as count").Group("enrollment_token_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		counts[row.EnrollmentTokenID] = row.Count
	}
	return counts, nil
}

func (r *agentRepository) SetConflicts(ctx context.Context, conflicts map[uuid.UUID][]string, at time.Time) error {
	db := r.db.WithContext(ctx)
	ids := make([]uuid.UUID, 0, len(conflicts))
	for id := range conflicts {
		ids = append(ids, id)
	}
	solved := db.Model(&model.Agent{}).Where("conflict_at is not null")
	if len(ids) > 0 {
		solved = solved.Where("id not in ?", ids)
	}
	if err := solved.Updates(map[string]any{"conflict_at": nil, "conflict_hostnames": nil}).Error; err != nil {
		return err
	}
	for id, hostnames := range conflicts {
		err := db.Model(&model.Agent{}).Where("id = ?", id).Updates(map[string]any{
			"conflict_at":        gorm.Expr("coalesce(conflict_at, ?)", at),
			"conflict_hostnames": model.StringArray(hostnames),
		}).Error
		if err != nil {
			return err
		}
	}
	return nil
}
