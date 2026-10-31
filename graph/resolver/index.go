package resolver

import (
	"context"
	"slices"
	"sync"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// index is the inventory of a namespace with its rows by id, by host and
// by link end, so that each field resolves without a query.
type index struct {
	inv          *model.Inventory
	projects     map[uuid.UUID]*model.Project
	envs         map[uuid.UUID]*model.Environment
	networks     map[uuid.UUID]*model.Network
	hosts        map[uuid.UUID]*model.Host
	apps         map[uuid.UUID]*model.App
	services     map[uuid.UUID]*model.Service
	webservers   map[uuid.UUID]*model.Webserver
	cidrs        map[uuid.UUID][]model.NetworkCIDR
	nets         *linker.Networks
	ips          map[uuid.UUID][]model.HostIP
	runtimesOf   map[uuid.UUID][]model.Runtime
	servicesOf   map[uuid.UUID][]model.Service
	webserversOf map[uuid.UUID][]model.Webserver
	appsOf       map[uuid.UUID][]model.App
	dependencies map[uuid.UUID][]model.AppDependency
	domains      map[uuid.UUID][]string
	linksFrom    map[uuid.UUID][]model.Link
	linksTo      map[uuid.UUID][]model.Link
	// hostsIn are the hosts of each network.
	hostsIn map[uuid.UUID][]*model.Host
}

func newIndex(inv *model.Inventory) *index {
	x := &index{inv: inv,
		projects: map[uuid.UUID]*model.Project{}, envs: map[uuid.UUID]*model.Environment{}, networks: map[uuid.UUID]*model.Network{},
		hosts: map[uuid.UUID]*model.Host{}, apps: map[uuid.UUID]*model.App{}, services: map[uuid.UUID]*model.Service{},
		webservers: map[uuid.UUID]*model.Webserver{}, cidrs: map[uuid.UUID][]model.NetworkCIDR{}, ips: map[uuid.UUID][]model.HostIP{},
		runtimesOf: map[uuid.UUID][]model.Runtime{}, servicesOf: map[uuid.UUID][]model.Service{},
		webserversOf: map[uuid.UUID][]model.Webserver{}, appsOf: map[uuid.UUID][]model.App{},
		dependencies: map[uuid.UUID][]model.AppDependency{}, domains: map[uuid.UUID][]string{},
		linksFrom: map[uuid.UUID][]model.Link{}, linksTo: map[uuid.UUID][]model.Link{},
		nets: linker.NewNetworks(inv), hostsIn: map[uuid.UUID][]*model.Host{}}
	for i := range inv.Projects {
		x.projects[inv.Projects[i].ID] = &inv.Projects[i]
	}
	for i := range inv.Environments {
		x.envs[inv.Environments[i].ID] = &inv.Environments[i]
	}
	for i := range inv.Networks {
		x.networks[inv.Networks[i].ID] = &inv.Networks[i]
	}
	for _, c := range inv.CIDRs {
		x.cidrs[c.NetworkID] = append(x.cidrs[c.NetworkID], c)
	}
	for i := range inv.Hosts {
		x.hosts[inv.Hosts[i].ID] = &inv.Hosts[i]
	}
	for _, ip := range inv.IPs {
		x.ips[ip.HostID] = append(x.ips[ip.HostID], ip)
		if n, ok := x.nets.Of(ip.HostID, ip.IP.Addr); ok && !slices.ContainsFunc(x.hostsIn[n], func(h *model.Host) bool { return h.ID == ip.HostID }) {
			if h := x.hosts[ip.HostID]; h != nil {
				x.hostsIn[n] = append(x.hostsIn[n], h)
			}
		}
	}
	for _, r := range inv.Runtimes {
		x.runtimesOf[r.HostID] = append(x.runtimesOf[r.HostID], r)
	}
	for i, s := range inv.Services {
		x.services[s.ID] = &inv.Services[i]
		x.servicesOf[s.HostID] = append(x.servicesOf[s.HostID], s)
	}
	for i, w := range inv.Webservers {
		x.webservers[w.ID] = &inv.Webservers[i]
		x.webserversOf[w.HostID] = append(x.webserversOf[w.HostID], w)
	}
	for i, a := range inv.Apps {
		x.apps[a.ID] = &inv.Apps[i]
		x.appsOf[a.HostID] = append(x.appsOf[a.HostID], a)
	}
	for _, d := range inv.Dependencies {
		x.dependencies[d.AppID] = append(x.dependencies[d.AppID], d)
	}
	for _, e := range inv.Endpoints {
		if e.Kind == model.EndpointKindDomain && e.OwnerType == model.EndpointOwnerWebserver {
			x.domains[e.OwnerID] = append(x.domains[e.OwnerID], e.Address)
		}
	}
	for _, l := range inv.Links {
		x.linksFrom[l.FromID] = append(x.linksFrom[l.FromID], l)
		if l.ToID != nil {
			x.linksTo[*l.ToID] = append(x.linksTo[*l.ToID], l)
		}
	}
	return x
}

// network is the network of an IP: the one holding the most specific CIDR
// containing it, or the one split from it its host was moved into.
func (x *index) network(ip model.HostIP) *model.Network {
	id, ok := x.nets.Of(ip.HostID, ip.IP.Addr)
	if !ok {
		return nil
	}
	return x.networks[id]
}

// cache holds the indexes loaded by a request, and the namespace of each
// host, so that a HostIP finds its index.
type cache struct {
	mu      sync.Mutex
	indexes map[uuid.UUID]*index
	hostNS  map[uuid.UUID]uuid.UUID
}

type cacheKey struct{}

// WithCache gives a request its cache; the handler wraps every operation
// with it.
func WithCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheKey{}, &cache{indexes: map[uuid.UUID]*index{}, hostNS: map[uuid.UUID]uuid.UUID{}})
}

// AroundOperations gives each operation its cache.
func AroundOperations(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
	return next(WithCache(ctx))
}

// index returns the index of the namespace, loaded once per request.
func (r *Resolver) index(ctx context.Context, namespaceID uuid.UUID) (*index, error) {
	c, ok := ctx.Value(cacheKey{}).(*cache)
	if !ok {
		c = &cache{indexes: map[uuid.UUID]*index{}, hostNS: map[uuid.UUID]uuid.UUID{}}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if x, ok := c.indexes[namespaceID]; ok {
		return x, nil
	}
	inv, err := r.NamespaceService.Inventory(ctx, namespaceID)
	if err != nil {
		return nil, err
	}
	x := newIndex(inv)
	c.indexes[namespaceID] = x
	for id := range x.hosts {
		c.hostNS[id] = namespaceID
	}
	return x, nil
}

// hostIndex returns the index holding the host, among those of the
// request.
func (r *Resolver) hostIndex(ctx context.Context, hostID uuid.UUID) *index {
	c, ok := ctx.Value(cacheKey{}).(*cache)
	if !ok {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.indexes[c.hostNS[hostID]]
}

// forget drops the index of the namespace loaded by the request: a
// mutation changed it.
func (r *Resolver) forget(ctx context.Context, namespaceID uuid.UUID) {
	if c, ok := ctx.Value(cacheKey{}).(*cache); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.indexes, namespaceID)
	}
}

// forgetAll drops every index loaded by the request.
func (r *Resolver) forgetAll(ctx context.Context) {
	if c, ok := ctx.Value(cacheKey{}).(*cache); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.indexes = map[uuid.UUID]*index{}
	}
}
