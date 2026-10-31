package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/alexandreh2ag/lacis/repository"
	"gorm.io/gorm"
)

// ErrNamespaceMismatch is returned when a snapshot names another namespace
// than the one of its host.
var ErrNamespaceMismatch = errors.New("namespace mismatch")

// IngestResult tells what was done with a snapshot.
type IngestResult int

const (
	// Accepted: the snapshot is stored and its normalization queued.
	Accepted IngestResult = iota
	// Unchanged: the snapshot is identical to the previous one, only the
	// last seen dates moved.
	Unchanged
)

type SnapshotService interface {
	// Authenticate returns the agent of the token, and ErrUnauthorized when
	// the token is unknown or revoked.
	Authenticate(ctx context.Context, agentToken string) (*model.Agent, error)
	// Ingest stores the snapshot s of agent, raw being its JSON as received.
	// It returns ErrInvalid for a snapshot to reject and
	// ErrNamespaceMismatch when its namespace is not the one of its host.
	Ingest(ctx context.Context, agent *model.Agent, s *snapshot.Snapshot, raw json.RawMessage) (IngestResult, error)
}

type snapshotService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
	agents repository.AgentRepository
	queue  queue.Queue
}

func NewSnapshotService(appCtx *appContext.Context, tx repository.Transactor, agents repository.AgentRepository, q queue.Queue) SnapshotService {
	return &snapshotService{appCtx: appCtx, tx: tx, agents: agents, queue: q}
}

func (s *snapshotService) Authenticate(ctx context.Context, agentToken string) (*model.Agent, error) {
	agent, err := s.agents.FindByTokenHash(ctx, hash.Token(agentToken))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if agent.RevokedAt != nil {
		return nil, ErrUnauthorized
	}
	return agent, nil
}

func (s *snapshotService) Ingest(ctx context.Context, agent *model.Agent, snap *snapshot.Snapshot, raw json.RawMessage) (IngestResult, error) {
	if !slices.Contains(s.appCtx.Config.AgentAPI.AcceptedSchemaVersions, snap.SchemaVersion) {
		return 0, fmt.Errorf("%w: schema version %d is not accepted", ErrInvalid, snap.SchemaVersion)
	}
	if err := snap.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	snapHash, err := snap.Hash()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	now := s.appCtx.Clock.Now()

	result := Accepted
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		host, err := repos.Host.FindByAgentID(ctx, agent.ID)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			host, err = s.newHost(ctx, repos, agent, snap, now)
		case err == nil:
			err = s.checkNamespace(ctx, repos, host, snap)
		}
		if err != nil {
			return err
		}
		if err = repos.Agent.Touch(ctx, agent.ID, now); err != nil {
			return err
		}
		host.LastSeenAt = now
		if host.LastSnapshotHash != nil && *host.LastSnapshotHash == snapHash {
			result = Unchanged
			return repos.Host.Update(ctx, host)
		}

		host.Hostname = snap.Host.Hostname
		host.Labels = snap.Host.Labels
		host.AgentVersion = &snap.Agent.Version
		host.LastSnapshotHash = &snapHash
		if err = repos.Host.Update(ctx, host); err != nil {
			return err
		}
		stored := &model.Snapshot{
			HostID:        host.ID,
			CollectedAt:   snap.CollectedAt,
			Hash:          snapHash,
			SchemaVersion: snap.SchemaVersion,
			Payload:       raw,
		}
		if err = repos.Snapshot.Create(ctx, stored); err != nil {
			return err
		}
		return s.queue.InsertNormalizeSnapshot(ctx, repos.DB, stored.ID)
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}

// newHost creates the host of the first snapshot of agent, and its
// namespace when missing.
func (s *snapshotService) newHost(ctx context.Context, repos *repository.Repositories, agent *model.Agent, snap *snapshot.Snapshot, now time.Time) (*model.Host, error) {
	namespace, err := findOrCreateNamespace(ctx, repos, s.appCtx, snap.Namespace)
	if err != nil {
		return nil, err
	}
	host := &model.Host{
		AgentID:     agent.ID,
		NamespaceID: namespace.ID,
		Hostname:    snap.Host.Hostname,
		Labels:      snap.Host.Labels,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	if err := repos.Host.Create(ctx, host); err != nil {
		return nil, err
	}
	s.appCtx.Logger.Info("host created", "host_id", host.ID, "hostname", host.Hostname, "namespace", namespace.Slug, "agent_id", agent.ID)
	return host, nil
}

// checkNamespace rejects a snapshot naming another namespace than the one
// of its host: moving a host is not supported yet.
func (s *snapshotService) checkNamespace(ctx context.Context, repos *repository.Repositories, host *model.Host, snap *snapshot.Snapshot) error {
	namespace, err := repos.Namespace.FindByID(ctx, host.NamespaceID)
	if err != nil {
		return err
	}
	if namespace.Slug != snap.Namespace {
		return fmt.Errorf("%w: the host is in namespace %q, the snapshot names %q", ErrNamespaceMismatch, namespace.Slug, snap.Namespace)
	}
	return nil
}

// findOrCreateNamespace returns the namespace slug, created with the
// default thresholds when missing.
func findOrCreateNamespace(ctx context.Context, repos *repository.Repositories, appCtx *appContext.Context, slug string) (*model.Namespace, error) {
	namespace, err := repos.Namespace.FindBySlug(ctx, slug)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return namespace, err
	}
	defaults := appCtx.Config.Defaults
	namespace = &model.Namespace{
		Slug:              slug,
		Name:              slug,
		StaleAfter:        model.Interval(defaults.StaleAfter),
		InactiveAfter:     model.Interval(defaults.InactiveAfter),
		SnapshotRetention: model.Interval(defaults.SnapshotRetention),
	}
	if err := repos.Namespace.Create(ctx, namespace); err != nil {
		return nil, err
	}
	appCtx.Logger.Info("namespace created", "namespace", slug)
	return namespace, nil
}
