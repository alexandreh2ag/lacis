package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/history"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PublicNetworkSlug is the network of the public IPs of a namespace.
const PublicNetworkSlug = "public"

// DefaultInstance is the instance of a service running once on its host.
const DefaultInstance = "default"

// NormalizeService turns a stored snapshot into the tables of its host.
type NormalizeService interface {
	// Normalize normalizes the snapshot id, unless a newer snapshot of its
	// host was stored since: the latest snapshot is the truth. It returns
	// the namespace of the host, whose links are to compute again; uuid.Nil
	// when the snapshot was skipped.
	Normalize(ctx context.Context, id int64) (uuid.UUID, error)
	// NormalizeAll normalizes again the latest snapshot of every host, as
	// after a change of the normalization; it returns how many it did.
	NormalizeAll(ctx context.Context) (int, error)
}

type normalizeService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewNormalizeService(appCtx *appContext.Context, tx repository.Transactor) NormalizeService {
	return &normalizeService{appCtx: appCtx, tx: tx}
}

func (s *normalizeService) Normalize(ctx context.Context, id int64) (uuid.UUID, error) {
	namespaceID := uuid.Nil
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		stored, err := repos.Snapshot.FindByID(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // purged, or its host deleted
		}
		if err != nil {
			return err
		}
		// Two normalizations of the host would interleave their writes, the
		// older one maybe last: the second waits, then finds whether its
		// snapshot is still the latest.
		if err := repos.Lock.Lock(ctx, NormalizeLockKey(stored.HostID)); err != nil {
			return err
		}
		latest, err := repos.Snapshot.FindLatestByHost(ctx, stored.HostID)
		if err != nil {
			return err
		}
		if latest.ID != stored.ID {
			s.appCtx.Logger.Debug("snapshot superseded, not normalized", "snapshot_id", id, "latest_id", latest.ID)
			return nil
		}
		host, err := repos.Host.FindByID(ctx, stored.HostID)
		if err != nil {
			return err
		}
		snap := &snapshot.Snapshot{}
		if err := json.Unmarshal(stored.Payload, snap); err != nil {
			return fmt.Errorf("decode snapshot %d: %w", id, err)
		}
		now := s.appCtx.Clock.Now()
		// hostFields always fills the resources: a host without them was
		// never normalized.
		n := &normalizer{ctx: ctx, repos: repos, log: s.appCtx.Logger.With("host_id", host.ID, "snapshot_id", id),
			host: host, snap: snap, now: now, first: host.Resources == nil, rec: history.NewRecorder(host.NamespaceID, now),
			projects: map[string]uuid.UUID{}, envs: map[string]uuid.UUID{}}
		for _, step := range []func() error{n.hostFields, n.ips, n.runtimes, n.containers, n.services, n.webservers, n.apps} {
			if err := step(); err != nil {
				return fmt.Errorf("normalize snapshot %d: %w", id, err)
			}
		}
		namespaceID = host.NamespaceID
		if err := repos.Host.UpdateDescription(ctx, host); err != nil {
			return err
		}
		return repos.ChangeEvent.Create(ctx, n.rec.Events())
	})
	if err != nil {
		return uuid.Nil, err
	}
	return namespaceID, nil
}

// NormalizeLockKey is the lock held while a snapshot of the host is
// normalized.
func NormalizeLockKey(hostID uuid.UUID) string { return "normalize:" + hostID.String() }

func (s *normalizeService) NormalizeAll(ctx context.Context) (int, error) {
	var ids []int64
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		ids, err = repos.Snapshot.FindLatestIDs(ctx)
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	done := 0
	for _, id := range ids {
		if _, err := s.Normalize(ctx, id); err != nil {
			errs = append(errs, err)
			continue
		}
		done++
	}
	return done, errors.Join(errs...)
}

// normalizer normalizes one snapshot in one transaction.
type normalizer struct {
	ctx   context.Context
	repos *repository.Repositories
	log   *slog.Logger
	host  *model.Host
	snap  *snapshot.Snapshot
	now   time.Time
	// first tells the first normalization of the host: its content is
	// told by the event of the host, not one event per entity.
	first bool
	rec   *history.Recorder
	// projects and envs are the ids of the slugs met, ensured once.
	projects, envs map[string]uuid.UUID
}

// track records an entity saved: added when before is nil, the entity
// being new or removed until now, else its fields that changed.
func (n *normalizer) track(entityType string, id uuid.UUID, before, after history.State, identity ...string) {
	switch {
	case n.first:
	case before == nil:
		n.rec.Added(entityType, id, &n.host.ID, after)
	default:
		n.rec.Changed(entityType, id, &n.host.ID, before, after, identity...)
	}
}

// untrack records the entities active until now and not kept.
func untrack[T any](n *normalizer, entityType string, existing []T, keep []uuid.UUID, row func(*T) (uuid.UUID, *time.Time), state func(*T) history.State) {
	if n.first {
		return
	}
	for i := range existing {
		id, removedAt := row(&existing[i])
		if removedAt == nil && !slices.Contains(keep, id) {
			n.rec.Removed(entityType, id, &n.host.ID, state(&existing[i]))
		}
	}
}

func hostState(h *model.Host) history.State {
	return history.State{"hostname": h.Hostname, "os": h.OS, "kernel": h.Kernel, "arch": h.Arch,
		"virtualization": h.Virtualization, "resources": h.Resources}
}

func runtimeState(r *model.Runtime) history.State {
	return withPackageVersion(history.State{"type": r.Type, "path": r.Path, "version": r.Version, "manager": r.Manager}, r.PackageVersion)
}

func serviceState(s *model.Service) history.State {
	return withPackageVersion(history.State{"type": s.Type, "instance": s.Instance, "version": s.Version}, s.PackageVersion)
}

// withPackageVersion adds the package version to the state when there is
// one: a package patched by the distribution (8.1.34-8 → 8.1.34-9) is a
// change, a runtime without package says nothing of it.
func withPackageVersion(s history.State, packageVersion *string) history.State {
	if packageVersion != nil {
		s["package_version"] = *packageVersion
	}
	return s
}

func webserverState(w *model.Webserver) history.State {
	return history.State{"type": w.Type, "version": w.Version}
}

// appState follows the code deployed: any change of it is a deployment.
func appState(a *model.App) history.State {
	return history.State{"path": a.Path, "name": a.Name, "role": a.Role, "framework": a.Framework,
		"framework_version": a.FrameworkVersion, "runtime": deployedRuntime(a.Runtime), "code": a.Code}
}

// deployedRuntime is the runtime of an app without its package version: a
// package patched by the distribution is a change of the runtime or of the
// service, recorded there, not a deployment of the app.
func deployedRuntime(runtime json.RawMessage) any {
	var rt map[string]any
	if json.Unmarshal(runtime, &rt) != nil || rt == nil {
		return runtime
	}
	delete(rt, "package_version")
	return rt
}

// hostFields copies the description of the host, and ties it to the
// project and env of its labels.
func (n *normalizer) hostFields() error {
	h, sh := n.host, n.snap.Host
	before := hostState(h)
	h.OS = marshalOrNil(sh.OS)
	h.Kernel, h.Arch, h.Virtualization = optional(sh.Kernel), optional(sh.Arch), optional(sh.Virtualization)
	h.Resources = marshalOrNil(map[string]any{"cpu": sh.CPU, "memory_mb": sh.MemoryMB, "disks": sh.Disks})
	project, env, err := n.catalog(sh.Labels.Project, sh.Labels.Env)
	if err != nil {
		return err
	}
	h.ProjectID, h.EnvironmentID = project, env
	// Nothing forbids two machines of the same hostname: their content
	// stays apart, but whatever finds a host by its name sees one of them.
	twins, err := n.repos.Host.CountByHostname(n.ctx, h.NamespaceID, h.Hostname, h.ID)
	if err != nil {
		return err
	}
	if twins > 0 {
		n.log.Warn("another host of the namespace has the same hostname", "hostname", h.Hostname, "others", twins)
	}
	if n.first {
		n.rec.Added(model.ChangeEntityHost, h.ID, &h.ID, hostState(h))
	} else {
		n.rec.Changed(model.ChangeEntityHost, h.ID, &h.ID, before, hostState(h), "hostname")
	}
	return nil
}

// catalog returns the project and env of these slugs, created on first
// sight; nil for an empty slug.
func (n *normalizer) catalog(project, env string) (projectID, envID *uuid.UUID, err error) {
	if project != "" {
		id, ok := n.projects[project]
		if !ok {
			p, err := n.repos.Project.Ensure(n.ctx, n.host.NamespaceID, project)
			if err != nil {
				return nil, nil, err
			}
			id = p.ID
			n.projects[project] = id
		}
		projectID = &id
	}
	if env != "" {
		id, ok := n.envs[env]
		if !ok {
			e, err := n.repos.Environment.Ensure(n.ctx, n.host.NamespaceID, env)
			if err != nil {
				return nil, nil, err
			}
			id = e.ID
			n.envs[env] = id
		}
		envID = &id
	}
	return projectID, envID, nil
}

// ips replaces the IPs of the host and gives each one a network: the
// known network holding it, else the public network for a public IP, else
// a network created for its subnet; an IP of a local interface such as
// docker0 gets none.
func (n *normalizer) ips() error {
	var ips []model.HostIP
	seen := map[netip.Addr]bool{}
	for _, iface := range n.snap.Host.Interfaces {
		for _, cidr := range iface.IPs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || seen[prefix.Addr()] {
				continue // the agent sends valid CIDRs; a duplicate keeps the first
			}
			seen[prefix.Addr()] = true
			name := iface.Name
			ips = append(ips, model.HostIP{HostID: n.host.ID, Interface: &name, IP: model.IP{Addr: prefix.Addr()},
				PrefixLen: prefix.Bits(), DefaultRoute: iface.DefaultRoute})
			if linker.IsLocalInterface(iface.Name) {
				continue // docker0 and the like never make a network shared by hosts
			}
			if err := n.network(prefix); err != nil {
				return err
			}
		}
	}
	return n.repos.HostIP.Replace(n.ctx, n.host.ID, ips)
}

func (n *normalizer) network(prefix netip.Prefix) error {
	ns := n.host.NamespaceID
	_, err := n.repos.Network.FindByIP(n.ctx, ns, prefix.Addr())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	subnet := prefix.Masked()
	network := &model.Network{NamespaceID: ns, Kind: model.NetworkKindPrivate, Slug: slugOf(subnet), Name: subnet.String()}
	if isPublic(prefix.Addr()) {
		network.Kind, network.Slug, network.Name = model.NetworkKindPublic, PublicNetworkSlug, "Public"
	}
	if err := n.repos.Network.Ensure(n.ctx, network); err != nil {
		return err
	}
	return n.repos.Network.AddCIDR(n.ctx, &model.NetworkCIDR{NetworkID: network.ID, NamespaceID: ns, CIDR: model.CIDR{Prefix: subnet}})
}

// isPublic tells an IP routed on the Internet: neither private (RFC 1918,
// ULA) nor loopback nor link-local.
func isPublic(ip netip.Addr) bool {
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

// separators are the runs of characters a slug cannot hold.
var separators = regexp.MustCompile(`[^a-z0-9]+`)

// slugOf names the network of a subnet: 10.0.1.0/24 is 10-0-1-0-24,
// fd00:1::/64 is fd00-1-64.
func slugOf(subnet netip.Prefix) string {
	return strings.Trim(separators.ReplaceAllString(strings.ToLower(subnet.String()), "-"), "-")
}

// runtimes synchronizes the runtimes, found by type and path.
func (n *normalizer) runtimes() error {
	existing, err := n.repos.Runtime.FindByHost(n.ctx, n.host.ID)
	if err != nil {
		return err
	}
	byKey := map[string]*model.Runtime{}
	for i := range existing {
		byKey[existing[i].Type+"\x00"+existing[i].Path] = &existing[i]
	}
	var keep []uuid.UUID
	seen := map[string]bool{}
	for _, r := range n.snap.Runtimes {
		if n.duplicate(seen, "runtime", r.Type+"\x00"+r.Path) {
			continue
		}
		var before history.State
		row, ok := byKey[r.Type+"\x00"+r.Path]
		if !ok {
			row = &model.Runtime{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, Type: r.Type, Path: r.Path, FirstSeenAt: n.now}
		} else if row.RemovedAt == nil {
			before = runtimeState(row)
		}
		row.Version, row.Manager, row.PackageVersion, row.RemovedAt = r.Version, optional(r.Manager), optional(r.PackageVersion), nil
		if err := n.repos.Runtime.Save(n.ctx, row); err != nil {
			return err
		}
		n.track(model.ChangeEntityRuntime, row.ID, before, runtimeState(row), "type", "path")
		keep = append(keep, row.ID)
	}
	untrack(n, model.ChangeEntityRuntime, existing, keep, func(r *model.Runtime) (uuid.UUID, *time.Time) { return r.ID, r.RemovedAt }, runtimeState)
	return n.repos.Runtime.MarkRemoved(n.ctx, n.host.ID, keep, n.now)
}

// containers synchronizes the Docker containers, found by name: a
// container created again under its name keeps its row. They make no
// change event: a container stopped or started is no change of the host.
func (n *normalizer) containers() error {
	existing, err := n.repos.Container.FindByHost(n.ctx, n.host.ID)
	if err != nil {
		return err
	}
	byName := map[string]*model.Container{}
	for i := range existing {
		byName[existing[i].Name] = &existing[i]
	}
	var keep []uuid.UUID
	seen := map[string]bool{}
	for _, c := range n.snap.Containers {
		if c.Name == "" || n.duplicate(seen, "container", c.Name) {
			continue
		}
		row, ok := byName[c.Name]
		if !ok {
			row = &model.Container{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, Name: c.Name, FirstSeenAt: n.now}
		}
		row.ContainerID, row.Image, row.Version, row.State = c.ID, c.Image, optional(c.Version), c.State
		row.Ports, row.Networks = c.Ports, c.Networks
		row.ComposeProject, row.ComposeService = optional(c.ComposeProject), optional(c.ComposeService)
		row.RemovedAt = nil
		if err := n.repos.Container.Save(n.ctx, row); err != nil {
			return err
		}
		keep = append(keep, row.ID)
	}
	return n.repos.Container.MarkRemoved(n.ctx, n.host.ID, keep, n.now)
}

// serviceDetails is what services.details holds of a service.
type serviceDetails struct {
	Listen     []string            `json:"listen,omitempty"`
	ConfigPath string              `json:"config_path,omitempty"`
	Cluster    string              `json:"cluster,omitempty"`
	Pools      []snapshot.FPMPool  `json:"pools,omitempty"`
	Container  *snapshot.Container `json:"container,omitempty"`
}

// services synchronizes the services, found by type and instance, and
// replaces their endpoints: one per address listened on.
func (n *normalizer) services() error {
	existing, err := n.repos.Service.FindByHost(n.ctx, n.host.ID)
	if err != nil {
		return err
	}
	byKey := map[string]*model.Service{}
	for i := range existing {
		byKey[existing[i].Type+"\x00"+existing[i].Instance] = &existing[i]
	}
	var keep []uuid.UUID
	var endpoints []model.Endpoint
	seen := map[string]bool{}
	for _, s := range n.snap.Services {
		instance := s.Instance
		if instance == "" {
			instance = DefaultInstance
		}
		if n.duplicate(seen, "service", s.Type+"\x00"+instance) {
			continue
		}
		var before history.State
		row, ok := byKey[s.Type+"\x00"+instance]
		if !ok {
			row = &model.Service{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, Type: s.Type, Instance: instance, FirstSeenAt: n.now}
		} else if row.RemovedAt == nil {
			before = serviceState(row)
		}
		row.Version, row.PackageVersion, row.RemovedAt = optional(s.Version), optional(s.PackageVersion), nil
		row.Details = marshalOrNil(serviceDetails{Listen: s.Listen, ConfigPath: s.ConfigPath, Cluster: s.Cluster, Pools: s.Pools, Container: s.Container})
		if err := n.repos.Service.Save(n.ctx, row); err != nil {
			return err
		}
		n.track(model.ChangeEntityService, row.ID, before, serviceState(row), "type", "instance")
		keep = append(keep, row.ID)
		addresses := slices.Clone(s.Listen)
		for _, p := range s.Pools {
			addresses = append(addresses, p.Listen)
		}
		endpoints = append(endpoints, n.endpoints(addresses, model.EndpointOwnerService, row.ID)...)
	}
	untrack(n, model.ChangeEntityService, existing, keep, func(s *model.Service) (uuid.UUID, *time.Time) { return s.ID, s.RemovedAt }, serviceState)
	if err := n.repos.Service.MarkRemoved(n.ctx, n.host.ID, keep, n.now); err != nil {
		return err
	}
	return n.repos.Endpoint.Replace(n.ctx, n.host.ID, model.EndpointOwnerService, endpoints)
}

// webserverRouting is what webservers.routing holds: the routing of the
// snapshot, by kind of webserver.
type webserverRouting struct {
	Vhosts    []snapshot.Vhost        `json:"vhosts,omitempty"`
	Upstreams []snapshot.Upstream     `json:"upstreams,omitempty"`
	Routers   []snapshot.Router       `json:"routers,omitempty"`
	Services  []snapshot.ProxyService `json:"services,omitempty"`
	Frontends []snapshot.Frontend     `json:"frontends,omitempty"`
	Backends  []snapshot.Backend      `json:"backends,omitempty"`
}

// webservers synchronizes the webservers, found by type, and replaces
// their endpoints: the domains they serve and the addresses they listen
// on.
func (n *normalizer) webservers() error {
	existing, err := n.repos.Webserver.FindByHost(n.ctx, n.host.ID)
	if err != nil {
		return err
	}
	byType := map[string]*model.Webserver{}
	for i := range existing {
		byType[existing[i].Type] = &existing[i]
	}
	var keep []uuid.UUID
	var endpoints []model.Endpoint
	seen := map[string]bool{}
	for _, ws := range n.snap.Webservers {
		if n.duplicate(seen, "webserver", ws.Type) {
			continue
		}
		var before history.State
		row, ok := byType[ws.Type]
		if !ok {
			row = &model.Webserver{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, Type: ws.Type, FirstSeenAt: n.now}
		} else if row.RemovedAt == nil {
			before = webserverState(row)
		}
		row.Version, row.RemovedAt = optional(ws.Version), nil
		row.Routing = marshalOrNil(webserverRouting{Vhosts: ws.Vhosts, Upstreams: ws.Upstreams, Routers: ws.Routers,
			Services: ws.Services, Frontends: ws.Frontends, Backends: ws.Backends})
		if row.Routing == nil {
			row.Routing = json.RawMessage(`{}`)
		}
		if err := n.repos.Webserver.Save(n.ctx, row); err != nil {
			return err
		}
		n.track(model.ChangeEntityWebserver, row.ID, before, webserverState(row), "type")
		keep = append(keep, row.ID)
		listen, domains := slices.Clone(ws.Listen), []string(nil)
		for _, v := range ws.Vhosts {
			listen = append(listen, v.Listen...)
			domains = append(domains, v.ServerNames...)
		}
		for _, r := range ws.Routers {
			domains = append(domains, ruleDomains(r.Rule)...)
		}
		for _, f := range ws.Frontends {
			listen = append(listen, f.Bind...)
		}
		endpoints = append(endpoints, n.endpoints(listen, model.EndpointOwnerWebserver, row.ID)...)
		endpoints = append(endpoints, n.domains(domains, row.ID)...)
	}
	untrack(n, model.ChangeEntityWebserver, existing, keep, func(w *model.Webserver) (uuid.UUID, *time.Time) { return w.ID, w.RemovedAt }, webserverState)
	if err := n.repos.Webserver.MarkRemoved(n.ctx, n.host.ID, keep, n.now); err != nil {
		return err
	}
	return n.repos.Endpoint.Replace(n.ctx, n.host.ID, model.EndpointOwnerWebserver, endpoints)
}

var (
	ruleHost      = regexp.MustCompile(`\bHost(?:SNI)?\(([^)]*)\)`)
	ruleHostRegex = regexp.MustCompile(`\bHost(?:SNI)?Regexp\(([^)]*)\)`)
	backquoted    = regexp.MustCompile("`([^`]*)`")
)

// ruleDomains reads the domains of a Traefik rule: Host(`a`) or, in
// Traefik 2, Host(`a`, `b`); a HostRegexp gives its regex prefixed with
// "~", as nginx writes a regex server name. A HostSNI(`*`) gives none.
func ruleDomains(rule string) []string {
	var out []string
	for _, m := range ruleHostRegex.FindAllStringSubmatch(rule, -1) {
		for _, q := range backquoted.FindAllStringSubmatch(m[1], -1) {
			out = append(out, "~"+q[1])
		}
	}
	for _, m := range ruleHost.FindAllStringSubmatch(rule, -1) {
		for _, q := range backquoted.FindAllStringSubmatch(m[1], -1) {
			if q[1] != "*" {
				out = append(out, q[1])
			}
		}
	}
	return out
}

// domains gives one domain endpoint per name, lower case, once each.
func (n *normalizer) domains(names []string, ownerID uuid.UUID) []model.Endpoint {
	var out []model.Endpoint
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSuffix(name, "."))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, model.Endpoint{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, Kind: model.EndpointKindDomain,
			Address: name, OwnerType: model.EndpointOwnerWebserver, OwnerID: ownerID})
	}
	return out
}

// apps synchronizes the apps, found by path, with their project and env,
// and replaces their dependencies and their endpoints: one per address
// listened on. An app new at its path takes the row of the app of the
// same name and project on the host that is no longer at its own: a
// deployment into a new directory (builds/app/production/2-19-0) moves
// the app rather than replacing it.
func (n *normalizer) apps() error {
	existing, err := n.repos.App.FindByHost(n.ctx, n.host.ID)
	if err != nil {
		return err
	}
	byPath := map[string]*model.App{}
	for i := range existing {
		byPath[existing[i].Path] = &existing[i]
	}
	type incoming struct {
		app          snapshot.App
		project, env *uuid.UUID
		row          *model.App
	}
	var in []incoming
	claimed := map[uuid.UUID]bool{}
	seen := map[string]bool{}
	for _, a := range n.snap.Apps {
		if n.duplicate(seen, "app", a.Path) {
			continue
		}
		project, env, err := n.catalog(n.appSlug(a, labels.KeyProject, a.Project), n.appSlug(a, labels.KeyEnv, a.Env))
		if err != nil {
			return err
		}
		row := byPath[a.Path]
		if row != nil {
			claimed[row.ID] = true
		}
		in = append(in, incoming{app: a, project: project, env: env, row: row})
	}
	for i := range in {
		if in[i].row == nil {
			if row := moved(existing, claimed, in[i].app.Name, in[i].project); row != nil {
				claimed[row.ID] = true
				in[i].row = row
			}
		}
	}
	var keep []uuid.UUID
	var deps []model.AppDependency
	var endpoints []model.Endpoint
	for _, x := range in {
		a, row := x.app, x.row
		var before history.State
		if row == nil {
			row = &model.App{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, FirstSeenAt: n.now}
		} else if row.RemovedAt == nil {
			before = appState(row)
		}
		row.Path = a.Path
		row.ProjectID, row.EnvironmentID, row.RemovedAt = x.project, x.env, nil
		row.ResolvedPath, row.Name, row.Role = optional(a.ResolvedPath), a.Name, string(a.Role)
		row.Framework, row.FrameworkVersion = nil, nil
		if a.Framework != nil {
			row.Framework, row.FrameworkVersion = optional(a.Framework.Name), optional(a.Framework.Version)
		}
		row.Runtime, row.Code, row.Info = marshalOrNil(a.Runtime), marshalOrNil(a.Code), marshalOrNil(a.Info)
		row.Launchers = marshalOrNil(a.Launchers)
		if row.Launchers == nil {
			row.Launchers = json.RawMessage(`[]`)
		}
		row.Vhosts = model.StringArray(a.Vhosts)
		if err := n.repos.App.Save(n.ctx, row); err != nil {
			return err
		}
		n.track(model.ChangeEntityApp, row.ID, before, appState(row), "path", "name")
		keep = append(keep, row.ID)
		deps = append(deps, dependencies(row.ID, a.Dependencies)...)
		endpoints = append(endpoints, n.endpoints(a.Listen, model.EndpointOwnerApp, row.ID)...)
	}
	if err := n.repos.AppDependency.Replace(n.ctx, keep, deps); err != nil {
		return err
	}
	untrack(n, model.ChangeEntityApp, existing, keep, func(a *model.App) (uuid.UUID, *time.Time) { return a.ID, a.RemovedAt }, appState)
	if err := n.repos.App.MarkRemoved(n.ctx, n.host.ID, keep, n.now); err != nil {
		return err
	}
	return n.repos.Endpoint.Replace(n.ctx, n.host.ID, model.EndpointOwnerApp, endpoints)
}

// moved finds the row an app new at its path takes: the app of the host
// of the same name and project, in a project, that the snapshot no longer
// has at its path. A present app comes before a removed one, the latest
// removed first.
func moved(existing []model.App, claimed map[uuid.UUID]bool, name string, project *uuid.UUID) *model.App {
	if project == nil {
		return nil
	}
	var best *model.App
	for i := range existing {
		r := &existing[i]
		if claimed[r.ID] || r.Name != name || r.ProjectID == nil || *r.ProjectID != *project {
			continue
		}
		switch {
		case best == nil:
			best = r
		case best.RemovedAt != nil && (r.RemovedAt == nil || r.RemovedAt.After(*best.RemovedAt)):
			best = r
		}
	}
	return best
}

// duplicate tells whether the key of an entity was met already in the
// snapshot, recording it otherwise. The rows are unique by key: the first
// entity of a key is kept and the others dropped, rather than failing the
// snapshot, as the next ones of the host would fail too. Two containers
// running their app in /app are such a case.
func (n *normalizer) duplicate(seen map[string]bool, entityType, key string) bool {
	if seen[key] {
		n.log.Warn("duplicate in the snapshot, the first one kept", "entity", entityType,
			"key", strings.ReplaceAll(key, "\x00", " "))
		return true
	}
	seen[key] = true
	return false
}

// appSlug returns the project or env of an app, value, when it is a slug:
// the agent checks it, but an older or a forged one may send anything. An
// invalid value is logged and replaced by the label of the host.
func (n *normalizer) appSlug(a snapshot.App, key, value string) string {
	if value == "" || labels.IsSlug(value) {
		return value
	}
	fallback := n.snap.Host.Labels.Project
	if key == labels.KeyEnv {
		fallback = n.snap.Host.Labels.Env
	}
	if n.log != nil {
		n.log.Warn("app label is not a slug, host label used", "app", a.Path, "label", key, "fallback", fallback)
	}
	return fallback
}

// dependencies maps the dependencies of an app to rows.
func dependencies(appID uuid.UUID, deps []snapshot.Dependency) []model.AppDependency {
	out := make([]model.AppDependency, 0, len(deps))
	for _, d := range deps {
		row := model.AppDependency{AppID: appID, Key: d.Key, Scheme: optional(d.Scheme), Path: optional(d.Path),
			Target: marshalOrNil(d.Target), Source: marshalOrNil(d.Source), Unparsed: d.Unparsed}
		if d.Port != 0 {
			port := d.Port
			row.Port = &port
		}
		out = append(out, row)
	}
	return out
}

// endpoints turns listen addresses ("0.0.0.0:5432", "[::]:9200",
// "unix:/run/php/x.sock") into endpoints, once each; a bare port or a
// name is skipped.
func (n *normalizer) endpoints(addresses []string, ownerType string, ownerID uuid.UUID) []model.Endpoint {
	var out []model.Endpoint
	seen := map[string]bool{}
	for _, a := range addresses {
		e := model.Endpoint{NamespaceID: n.host.NamespaceID, HostID: n.host.ID, OwnerType: ownerType, OwnerID: ownerID}
		if socket, ok := strings.CutPrefix(a, "unix:"); ok {
			e.Kind, e.Address = model.EndpointKindUnix, socket
		} else {
			host, portStr, err := net.SplitHostPort(a)
			port, perr := strconv.Atoi(portStr)
			if err != nil || perr != nil || host == "" {
				continue
			}
			if ip, err := netip.ParseAddr(host); err == nil {
				host = ip.String()
			}
			e.Kind, e.Address, e.Port = model.EndpointKindTCP, host, &port
		}
		key := e.Kind + " " + e.Address + " " + strconv.Itoa(deref(e.Port))
		if !seen[key] {
			seen[key] = true
			out = append(out, e)
		}
	}
	return out
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// marshalOrNil encodes v, nil when v is nil.
func marshalOrNil(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return b
}
