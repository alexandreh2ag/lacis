package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/cdn"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/history"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
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
	opts   linker.Options
}

// NewLinkService fails on an unknown CDN or an invalid CDN range of the
// config, which the validation of the config already rejects.
func NewLinkService(appCtx *appContext.Context, tx repository.Transactor) (LinkService, error) {
	s := &linkService{appCtx: appCtx, tx: tx, opts: linker.Options{HTTPPorts: appCtx.Config.Linking.HTTPPorts}}
	for _, name := range appCtx.Config.Linking.CDNs {
		c, ok := cdn.Known(name)
		if !ok {
			return nil, fmt.Errorf("linking.cdns: unknown CDN %q, one of %s", name, strings.Join(cdn.Names(), ", "))
		}
		s.opts.CDN = append(s.opts.CDN, c)
	}
	custom := cdn.CDN{Name: cdn.Custom}
	for _, r := range appCtx.Config.Linking.CDNRanges {
		prefix, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("linking.cdn_ranges: %w", err)
		}
		custom.Ranges = append(custom.Ranges, prefix.Masked())
	}
	if len(custom.Ranges) > 0 {
		s.opts.CDN = append(s.opts.CDN, custom)
	}
	return s, nil
}

func (s *linkService) Recompute(ctx context.Context, namespaceID uuid.UUID) (int, error) {
	stored := 0
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		// Two recomputations of the namespace would each replace the links
		// of the other: the second waits, then reads what the first wrote.
		if err := repos.Lock.Lock(ctx, LinksLockKey(namespaceID)); err != nil {
			return err
		}
		inv, err := repos.Inventory.Load(ctx, namespaceID)
		if err != nil {
			return err
		}
		now := s.appCtx.Clock.Now()
		links := linker.Resolve(inv, s.opts, now)
		linkIDs(namespaceID, links)
		stored = len(links)
		rec := history.NewRecorder(namespaceID, now)
		history.Links(rec, inv, links)
		if err := repos.Link.Replace(ctx, namespaceID, links); err != nil {
			return err
		}
		return repos.ChangeEvent.Create(ctx, rec.Events())
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

// LinksLockKey is the lock held while the links of the namespace are
// computed.
func LinksLockKey(namespaceID uuid.UUID) string { return "links:" + namespaceID.String() }

// linkNamespace is the UUID namespace of the ids of the links.
var linkNamespace = uuid.MustParse("6f1c2a64-1f0e-4f5b-9d6e-6c1b8e0a4d52")

// linkIDs gives each link an id derived from what identifies it: its ends,
// its source and the key of its dependency. A link computed again keeps
// its id, its row updated in place rather than deleted and inserted. Links
// sharing all that, such as two dependencies of the same key going to the
// same target, are told apart by their rank, links coming in a stable
// order.
func linkIDs(namespaceID uuid.UUID, links []model.Link) {
	seen := map[string]int{}
	for i := range links {
		l := &links[i]
		var d linker.Detail
		_ = json.Unmarshal(l.Detail, &d)
		to, external := "", ""
		if l.ToID != nil {
			to = l.ToID.String()
		}
		if l.ExternalTarget != nil {
			external = *l.ExternalTarget
		}
		key := strings.Join([]string{namespaceID.String(), l.FromType, l.FromID.String(), l.ToType, to, external, l.Source, d.Key}, "\x00")
		rank := seen[key]
		seen[key]++
		if rank > 0 {
			key += "\x00" + strconv.Itoa(rank)
		}
		l.ID = uuid.NewSHA1(linkNamespace, []byte(key))
	}
}
