package service

import (
	"context"
	"errors"
	"slices"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
)

// ConflictWindow is how far back detect_token_conflicts reads the
// snapshots.
const ConflictWindow = 24 * time.Hour

// MaintenanceService runs the scheduled tasks. Each is idempotent and
// returns how many rows it changed.
type MaintenanceService interface {
	// PurgeSnapshots deletes the snapshots out of the retention of their
	// namespace, but the latest of each host.
	PurgeSnapshots(ctx context.Context) (int64, error)
	// PurgeRemovedEntities deletes for good the content removed for longer
	// than workers.params.purge_removed_entities_after.
	PurgeRemovedEntities(ctx context.Context) (int64, error)
	// PurgeExpiredTokens deletes the enrollment tokens expired that
	// enrolled no agent, and the sessions expired; with them, the OAuth
	// codes and grants expired and the OAuth clients nobody authorized.
	PurgeExpiredTokens(ctx context.Context) (tokens, sessions int64, err error)
	// DetectTokenConflicts marks the agents whose snapshots, over
	// ConflictWindow, alternate between hostnames: two machines share
	// their token, a cloned host. It clears the marks no longer seen and
	// returns how many agents are marked.
	DetectTokenConflicts(ctx context.Context) (int, error)
	// PurgeHistory deletes the change events of each namespace beyond its
	// history.max_events latest, none when 0.
	PurgeHistory(ctx context.Context) (int64, error)
	// PurgeInactiveHosts deletes the hosts not seen for longer than
	// workers.params.purge_inactive_hosts_after, as a deletion by hand
	// does, and returns how many.
	PurgeInactiveHosts(ctx context.Context) (int, error)
	// PurgeUnusedProjects deletes the projects no host and no app (removed
	// ones aside) names any more, as a deletion by hand, and returns how
	// many.
	PurgeUnusedProjects(ctx context.Context) (int, error)
	// PurgeUnusedEnvironments does the same with the envs.
	PurgeUnusedEnvironments(ctx context.Context) (int, error)
}

type maintenanceService struct {
	appCtx   *appContext.Context
	tx       repository.Transactor
	metadata MetadataService
}

func NewMaintenanceService(appCtx *appContext.Context, tx repository.Transactor, metadata MetadataService) MaintenanceService {
	return &maintenanceService{appCtx: appCtx, tx: tx, metadata: metadata}
}

func (s *maintenanceService) PurgeSnapshots(ctx context.Context) (int64, error) {
	var n int64
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		n, err = repos.Snapshot.PurgeOutdated(ctx, s.appCtx.Clock.Now())
		return err
	})
	return n, err
}

func (s *maintenanceService) PurgeRemovedEntities(ctx context.Context) (int64, error) {
	var n int64
	before := s.appCtx.Clock.Now().Add(-s.appCtx.Config.Workers.Params.PurgeRemovedEntitiesAfter)
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		n, err = repos.Inventory.PurgeRemoved(ctx, before)
		return err
	})
	return n, err
}

func (s *maintenanceService) PurgeExpiredTokens(ctx context.Context) (tokens, sessions int64, err error) {
	now := s.appCtx.Clock.Now()
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		if tokens, err = repos.EnrollmentToken.PurgeExpired(ctx, now); err != nil {
			return err
		}
		if sessions, err = repos.Session.DeleteExpired(ctx, now); err != nil {
			return err
		}
		purged, err := repos.OAuth.Purge(ctx, now, now.Add(-OAuthUnauthorizedClientTTL))
		if err != nil {
			return err
		}
		if purged != (repository.OAuthPurged{}) {
			s.appCtx.Logger.Info("OAuth purged", "codes", purged.Codes, "grants", purged.Grants, "clients", purged.Clients)
		}
		return nil
	})
	return tokens, sessions, err
}

func (s *maintenanceService) DetectTokenConflicts(ctx context.Context) (int, error) {
	now := s.appCtx.Clock.Now()
	var n int
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		byAgent, err := repos.Snapshot.FindHostnames(ctx, now.Add(-ConflictWindow))
		if err != nil {
			return err
		}
		conflicts := map[uuid.UUID][]string{}
		for agent, hostnames := range byAgent {
			if shared := alternating(hostnames); shared != nil {
				conflicts[agent] = shared
				s.appCtx.Logger.Warn("two machines share an agent token", "agent_id", agent, "hostnames", shared)
			}
		}
		n = len(conflicts)
		return repos.Agent.SetConflicts(ctx, conflicts, now)
	})
	return n, err
}

// alternating returns the hostnames, sorted, of a sequence coming back to
// a hostname it left: A, B, A. A renamed host, A then B, does not.
func alternating(hostnames []string) []string {
	var seen []string
	back := false
	for i, h := range hostnames {
		if i > 0 && h == hostnames[i-1] {
			continue
		}
		if slices.Contains(seen, h) {
			back = true
		} else {
			seen = append(seen, h)
		}
	}
	if !back {
		return nil
	}
	slices.Sort(seen)
	return seen
}

func (s *maintenanceService) PurgeHistory(ctx context.Context) (int64, error) {
	keep := s.appCtx.Config.History.MaxEvents
	if keep <= 0 {
		return 0, nil
	}
	var n int64
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		namespaces, err := repos.Namespace.FindAll(ctx)
		if err != nil {
			return err
		}
		for _, ns := range namespaces {
			deleted, err := repos.ChangeEvent.Purge(ctx, ns.ID, keep)
			if err != nil {
				return err
			}
			n += deleted
		}
		return nil
	})
	return n, err
}

func (s *maintenanceService) PurgeInactiveHosts(ctx context.Context) (int, error) {
	before := s.appCtx.Clock.Now().Add(-s.appCtx.Config.Workers.Params.PurgeInactiveHostsAfter)
	var hosts []repository.HostRef
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		hosts, err = repos.Host.FindSeenBefore(ctx, before)
		return err
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hosts {
		err := s.metadata.DeleteHost(ctx, h.Namespace, h.ID)
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrHostActive):
			// Deleted meanwhile, or a snapshot came back: left alone.
			continue
		case err != nil:
			return n, err
		}
		s.appCtx.Logger.Info("inactive host purged", "host_id", h.ID, "hostname", h.Hostname, "namespace", h.Namespace)
		n++
	}
	return n, nil
}

func (s *maintenanceService) PurgeUnusedProjects(ctx context.Context) (int, error) {
	var deleted []model.Project
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		deleted, err = repos.Project.DeleteUnused(ctx)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, p := range deleted {
		s.appCtx.Logger.Info("unused project purged", "project", p.Slug, "namespace_id", p.NamespaceID)
	}
	return len(deleted), nil
}

func (s *maintenanceService) PurgeUnusedEnvironments(ctx context.Context) (int, error) {
	var deleted []model.Environment
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		deleted, err = repos.Environment.DeleteUnused(ctx)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, e := range deleted {
		s.appCtx.Logger.Info("unused environment purged", "environment", e.Slug, "namespace_id", e.NamespaceID)
	}
	return len(deleted), nil
}
