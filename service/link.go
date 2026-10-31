package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/history"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
)

// LinkService computes the links of the namespaces.
type LinkService interface {
	// Recompute computes again the links of the namespace, replacing them;
	// it returns how many it stored.
	Recompute(ctx context.Context, namespaceID uuid.UUID) (int, error)
	// RecomputeAll computes again the links of every namespace; it returns
	// how many links it stored.
	RecomputeAll(ctx context.Context) (int, error)
}

type linkService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
	cdn    []netip.Prefix
}

// NewLinkService fails on an invalid CDN range of the config, which the
// validation of the config already rejects.
func NewLinkService(appCtx *appContext.Context, tx repository.Transactor) (LinkService, error) {
	s := &linkService{appCtx: appCtx, tx: tx}
	for _, r := range appCtx.Config.Linking.CDNRanges {
		prefix, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("linking.cdn_ranges: %w", err)
		}
		s.cdn = append(s.cdn, prefix.Masked())
	}
	return s, nil
}

func (s *linkService) Recompute(ctx context.Context, namespaceID uuid.UUID) (int, error) {
	stored := 0
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		inv, err := repos.Inventory.Load(ctx, namespaceID)
		if err != nil {
			return err
		}
		now := s.appCtx.Clock.Now()
		links := linker.Resolve(inv, s.cdn, now)
		stored = len(links)
		rec := history.NewRecorder(namespaceID, now)
		history.Links(rec, inv, links)
		if err := repos.Link.Replace(ctx, namespaceID, links); err != nil {
			return err
		}
		return writeEvents(ctx, repos, namespaceID, rec.Events(), s.appCtx.Config.History.MaxEvents)
	})
	if err != nil {
		return 0, fmt.Errorf("recompute links of namespace %s: %w", namespaceID, err)
	}
	s.appCtx.Logger.Debug("links recomputed", "namespace_id", namespaceID, "links", stored)
	return stored, nil
}

func (s *linkService) RecomputeAll(ctx context.Context) (int, error) {
	var ids []uuid.UUID
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		namespaces, err := repos.Namespace.FindAll(ctx)
		for _, ns := range namespaces {
			ids = append(ids, ns.ID)
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	total := 0
	for _, id := range ids {
		n, err := s.Recompute(ctx, id)
		if err != nil {
			errs = append(errs, err)
		}
		total += n
	}
	return total, errors.Join(errs...)
}
