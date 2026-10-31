package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/history"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// ErrHostActive refuses to delete a host still sending snapshots: the next
// one would bring it back.
var ErrHostActive = errors.New("the host is active")

const (
	maxName        = 100
	maxDescription = 2000
)

// The inputs change the fields set; a description set empty is cleared.
// Logos are uploaded apart, see LogoService.
type NamespaceInput struct {
	Name              *string
	Description       *string
	StaleAfter        *time.Duration
	InactiveAfter     *time.Duration
	SnapshotRetention *time.Duration
}

type ProjectInput struct {
	Name        *string
	Description *string
}

type EnvironmentInput struct {
	Name *string
}

type NetworkInput struct {
	Name        *string
	Description *string
	// Kind is private or public.
	Kind *string
}

// MetadataService changes what people say of the inventory: names,
// descriptions, logos, the order of the envs, the freshness thresholds,
// and removes the hosts gone. What the agents collect is never changed.
type MetadataService interface {
	UpdateNamespace(ctx context.Context, slug string, in NamespaceInput) (*model.Namespace, error)
	UpdateProject(ctx context.Context, namespace, slug string, in ProjectInput) (*model.Project, error)
	UpdateEnvironment(ctx context.Context, namespace, slug string, in EnvironmentInput) (*model.Environment, error)
	// ReorderEnvironments orders the envs as slugs, which names each env of
	// the namespace once.
	ReorderEnvironments(ctx context.Context, namespace string, slugs []string) ([]model.Environment, error)
	UpdateNetwork(ctx context.Context, namespace, slug string, in NetworkInput) (*model.Network, error)
	// SplitNetwork creates the private network name, split from the network
	// of the slug, and moves the hosts into it: two networks on the same
	// range, which the IPs alone cannot tell apart. The hosts stay there
	// across their snapshots.
	SplitNetwork(ctx context.Context, namespace, slug, name string, hostIDs []uuid.UUID) (*model.Network, error)
	// MoveHostsToNetwork moves hosts into the network of the slug, from the
	// network it was split from or from another network split from it.
	MoveHostsToNetwork(ctx context.Context, namespace, slug string, hostIDs []uuid.UUID) (*model.Network, error)
	// DeleteNetwork removes a network split by hand: its hosts go back to
	// the network it was split from.
	DeleteNetwork(ctx context.Context, namespace, slug string) error
	// DeleteHost removes a stale or inactive host, its content and its
	// snapshots, records it in the history and enqueues the recomputation
	// of the links; ErrHostActive for a host seen within staleAfter.
	DeleteHost(ctx context.Context, namespace string, id uuid.UUID) error
}

type metadataService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
	queue  queue.Queue
}

func NewMetadataService(appCtx *appContext.Context, tx repository.Transactor, q queue.Queue) MetadataService {
	return &metadataService{appCtx: appCtx, tx: tx, queue: q}
}

// inNamespace runs fn in a transaction, with the namespace of the slug.
func (s *metadataService) inNamespace(ctx context.Context, slug string, fn func(repos *repository.Repositories, ns *model.Namespace) error) error {
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		ns, err := repos.Namespace.FindBySlug(ctx, slug)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNamespaceNotFound
		}
		if err != nil {
			return err
		}
		return fn(repos, ns)
	})
}

func (s *metadataService) UpdateNamespace(ctx context.Context, slug string, in NamespaceInput) (*model.Namespace, error) {
	var out *model.Namespace
	err := s.inNamespace(ctx, slug, func(repos *repository.Repositories, ns *model.Namespace) error {
		if err := setName(&ns.Name, in.Name); err != nil {
			return err
		}
		if err := setText(&ns.Description, in.Description, "description", maxDescription); err != nil {
			return err
		}
		for _, d := range []struct {
			field string
			to    *model.Interval
			from  *time.Duration
		}{{"staleAfter", &ns.StaleAfter, in.StaleAfter}, {"inactiveAfter", &ns.InactiveAfter, in.InactiveAfter},
			{"snapshotRetention", &ns.SnapshotRetention, in.SnapshotRetention}} {
			if d.from == nil {
				continue
			}
			if *d.from < time.Minute {
				return fmt.Errorf("%w: %s must be a minute or more", ErrInvalid, d.field)
			}
			*d.to = model.Interval(*d.from)
		}
		if ns.InactiveAfter.Duration() <= ns.StaleAfter.Duration() {
			return fmt.Errorf("%w: inactiveAfter must be longer than staleAfter", ErrInvalid)
		}
		out = ns
		return repos.Namespace.Update(ctx, ns)
	})
	return out, err
}

func (s *metadataService) UpdateProject(ctx context.Context, namespace, slug string, in ProjectInput) (*model.Project, error) {
	var out *model.Project
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		p, err := found(repos.Project.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if err := setName(&p.Name, in.Name); err != nil {
			return err
		}
		if err := setText(&p.Description, in.Description, "description", maxDescription); err != nil {
			return err
		}
		out = p
		return repos.Project.Update(ctx, p)
	})
	return out, err
}

func (s *metadataService) UpdateEnvironment(ctx context.Context, namespace, slug string, in EnvironmentInput) (*model.Environment, error) {
	var out *model.Environment
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		e, err := found(repos.Environment.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if err := setName(&e.Name, in.Name); err != nil {
			return err
		}
		out = e
		return repos.Environment.Update(ctx, e)
	})
	return out, err
}

func (s *metadataService) ReorderEnvironments(ctx context.Context, namespace string, slugs []string) ([]model.Environment, error) {
	var out []model.Environment
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		envs, err := repos.Environment.FindAll(ctx, ns.ID)
		if err != nil {
			return err
		}
		have := make([]string, 0, len(envs))
		for _, e := range envs {
			have = append(have, e.Slug)
		}
		want := slices.Clone(slugs)
		slices.Sort(have)
		slices.Sort(want)
		if !slices.Equal(have, want) {
			return fmt.Errorf("%w: the order must name each env once: %s", ErrInvalid, strings.Join(have, ", "))
		}
		for i := range envs {
			envs[i].SortOrder = slices.Index(slugs, envs[i].Slug)
			if err := repos.Environment.Update(ctx, &envs[i]); err != nil {
				return err
			}
		}
		slices.SortFunc(envs, func(a, b model.Environment) int { return a.SortOrder - b.SortOrder })
		out = envs
		return nil
	})
	return out, err
}

func (s *metadataService) UpdateNetwork(ctx context.Context, namespace, slug string, in NetworkInput) (*model.Network, error) {
	var out *model.Network
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		n, err := found(repos.Network.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if err := setName(&n.Name, in.Name); err != nil {
			return err
		}
		if err := setText(&n.Description, in.Description, "description", maxDescription); err != nil {
			return err
		}
		if in.Kind != nil {
			if *in.Kind != model.NetworkKindPrivate && *in.Kind != model.NetworkKindPublic {
				return fmt.Errorf("%w: kind is private or public", ErrInvalid)
			}
			n.Kind = *in.Kind
		}
		out = n
		return repos.Network.Update(ctx, n)
	})
	return out, err
}

func (s *metadataService) SplitNetwork(ctx context.Context, namespace, slug, name string, hostIDs []uuid.UUID) (*model.Network, error) {
	var out *model.Network
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		source, err := found(repos.Network.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if source.Kind == model.NetworkKindPublic {
			return fmt.Errorf("%w: a public network is not split", ErrInvalid)
		}
		split := &model.Network{NamespaceID: ns.ID, Kind: model.NetworkKindPrivate, SplitFromID: rootOf(source)}
		if err := setName(&split.Name, &name); err != nil {
			return err
		}
		if split.Slug = slugify(split.Name); split.Slug == "" {
			return fmt.Errorf("%w: the name must hold a letter or a digit", ErrInvalid)
		}
		if _, err := repos.Network.FindBySlug(ctx, ns.ID, split.Slug); !errors.Is(err, gorm.ErrRecordNotFound) {
			if err == nil {
				return fmt.Errorf("%w: network %s", ErrAlreadyExists, split.Slug)
			}
			return err
		}
		if err := checkMoved(ctx, repos, ns.ID, source, hostIDs, true); err != nil {
			return err
		}
		if err := repos.Network.Create(ctx, split); err != nil {
			return err
		}
		if err := repos.Network.MoveHosts(ctx, *split.SplitFromID, split.ID, hostIDs); err != nil {
			return err
		}
		out = split
		return s.queue.InsertRecomputeLinks(ctx, repos.DB, ns.ID)
	})
	return out, err
}

func (s *metadataService) MoveHostsToNetwork(ctx context.Context, namespace, slug string, hostIDs []uuid.UUID) (*model.Network, error) {
	var out *model.Network
	err := s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		target, err := found(repos.Network.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if err := checkMoved(ctx, repos, ns.ID, target, hostIDs, false); err != nil {
			return err
		}
		if err := repos.Network.MoveHosts(ctx, *rootOf(target), target.ID, hostIDs); err != nil {
			return err
		}
		out = target
		return s.queue.InsertRecomputeLinks(ctx, repos.DB, ns.ID)
	})
	return out, err
}

func (s *metadataService) DeleteNetwork(ctx context.Context, namespace, slug string) error {
	return s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		n, err := found(repos.Network.FindBySlug(ctx, ns.ID, slug))
		if err != nil {
			return err
		}
		if n.SplitFromID == nil {
			return fmt.Errorf("%w: only a network split by hand is deleted, the others come from the IPs", ErrInvalid)
		}
		if err := repos.Network.Delete(ctx, n.ID); err != nil {
			return err
		}
		return s.queue.InsertRecomputeLinks(ctx, repos.DB, ns.ID)
	})
}

// rootOf is the network whose CIDRs give the range of n: n itself, or the
// network n was split from.
func rootOf(n *model.Network) *uuid.UUID {
	if n.SplitFromID != nil {
		return n.SplitFromID
	}
	return &n.ID
}

// checkMoved refuses to move hosts into n unless each has an IP in the
// range of n, and is not in n yet; from the network split, inSource
// requires each host to be in n.
func checkMoved(ctx context.Context, repos *repository.Repositories, namespaceID uuid.UUID, n *model.Network, hostIDs []uuid.UUID, inSource bool) error {
	if len(hostIDs) == 0 {
		return fmt.Errorf("%w: no host to move", ErrInvalid)
	}
	inv, err := repos.Inventory.Load(ctx, namespaceID)
	if err != nil {
		return err
	}
	networks, root := linker.NewNetworks(inv), *rootOf(n)
	// in is the network of the range each host is in.
	in := map[uuid.UUID]uuid.UUID{}
	for _, ip := range inv.IPs {
		if byCIDR, ok := networks.ByCIDR(ip.IP.Addr); ok && byCIDR == root {
			in[ip.HostID], _ = networks.Of(ip.HostID, ip.IP.Addr)
		}
	}
	hostnames := map[uuid.UUID]string{}
	for _, h := range inv.Hosts {
		hostnames[h.ID] = h.Hostname
	}
	for _, id := range hostIDs {
		current, ok := in[id]
		switch {
		case hostnames[id] == "":
			return fmt.Errorf("%w: host %s", ErrNotFound, id)
		case !ok:
			return fmt.Errorf("%w: %s has no IP in the range of %s", ErrInvalid, hostnames[id], n.Slug)
		case inSource && current != n.ID:
			return fmt.Errorf("%w: %s is not in %s", ErrInvalid, hostnames[id], n.Slug)
		case !inSource && current == n.ID:
			return fmt.Errorf("%w: %s is already in %s", ErrInvalid, hostnames[id], n.Slug)
		}
	}
	return nil
}

// slugify makes a slug of a name: lower case, without accents, its other
// characters as dashes.
func slugify(name string) string {
	plain, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn))), name)
	if err != nil {
		plain = name
	}
	return strings.Trim(separators.ReplaceAllString(strings.ToLower(plain), "-"), "-")
}

func (s *metadataService) DeleteHost(ctx context.Context, namespace string, id uuid.UUID) error {
	now := s.appCtx.Clock.Now()
	return s.inNamespace(ctx, namespace, func(repos *repository.Repositories, ns *model.Namespace) error {
		h, err := found(repos.Host.FindByID(ctx, id))
		if err != nil {
			return err
		}
		if h.NamespaceID != ns.ID {
			return ErrNotFound
		}
		if now.Sub(h.LastSeenAt) <= ns.StaleAfter.Duration() {
			return ErrHostActive
		}
		if err := repos.Host.Delete(ctx, h.ID); err != nil {
			return err
		}
		rec := history.NewRecorder(ns.ID, now)
		rec.Removed(model.ChangeEntityHost, h.ID, &h.ID, history.State{"hostname": h.Hostname, "last_seen_at": h.LastSeenAt})
		if err := writeEvents(ctx, repos, ns.ID, rec.Events(), s.appCtx.Config.History.MaxEvents); err != nil {
			return err
		}
		return s.queue.InsertRecomputeLinks(ctx, repos.DB, ns.ID)
	})
}

// found turns a missing row into ErrNotFound.
func found[T any](row *T, err error) (*T, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return row, err
}

func setName(to *string, from *string) error {
	if from == nil {
		return nil
	}
	name := strings.TrimSpace(*from)
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return fmt.Errorf("%w: name must hold 1 to %d characters", ErrInvalid, maxName)
	}
	*to = name
	return nil
}

func setText(to **string, from *string, field string, limit int) error {
	if from == nil {
		return nil
	}
	text := strings.TrimSpace(*from)
	if utf8.RuneCountInString(text) > limit {
		return fmt.Errorf("%w: %s must hold %d characters at most", ErrInvalid, field, limit)
	}
	if text == "" {
		*to = nil
		return nil
	}
	*to = &text
	return nil
}
