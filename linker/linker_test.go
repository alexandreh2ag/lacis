package linker

import (
	"encoding/json"
	"fmt"
	cdnpkg "github.com/alexandreh2ag/lacis/cdn"
	"net/netip"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/pathmatch"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture builds an inventory by hand.
type fixture struct {
	inv *model.Inventory
}

func newFixture() *fixture {
	return &fixture{inv: &model.Inventory{Namespace: model.Namespace{Base: model.Base{ID: uuid.New()}}}}
}

func (f *fixture) network(cidrs ...string) uuid.UUID {
	id := uuid.New()
	for _, c := range cidrs {
		f.inv.CIDRs = append(f.inv.CIDRs, model.NetworkCIDR{NetworkID: id, CIDR: model.CIDR{Prefix: netip.MustParsePrefix(c)}})
	}
	return id
}

func (f *fixture) host(name string, ips ...string) uuid.UUID {
	id := uuid.New()
	f.inv.Hosts = append(f.inv.Hosts, model.Host{Base: model.Base{ID: id}, Hostname: name})
	for _, ip := range ips {
		f.inv.IPs = append(f.inv.IPs, model.HostIP{HostID: id, IP: model.IP{Addr: netip.MustParseAddr(ip)}})
	}
	return id
}

// iface adds IPs on an interface of a host.
func (f *fixture) iface(hostID uuid.UUID, name string, ips ...string) {
	for _, ip := range ips {
		f.inv.IPs = append(f.inv.IPs, model.HostIP{HostID: hostID, Interface: &name, IP: model.IP{Addr: netip.MustParseAddr(ip)}})
	}
}

func (f *fixture) listen(hostID uuid.UUID, ownerType string, ownerID uuid.UUID, kind, address string, port int) {
	e := model.Endpoint{HostID: hostID, Kind: kind, Address: address, OwnerType: ownerType, OwnerID: ownerID}
	if port != 0 {
		e.Port = &port
	}
	f.inv.Endpoints = append(f.inv.Endpoints, e)
}

func (f *fixture) service(hostID uuid.UUID, typ, kind, address string, port int) uuid.UUID {
	id := uuid.New()
	f.inv.Services = append(f.inv.Services, model.Service{Base: model.Base{ID: id}, HostID: hostID, Type: typ})
	f.listen(hostID, model.EndpointOwnerService, id, kind, address, port)
	return id
}

func (f *fixture) webserver(hostID uuid.UUID, typ string, rt routing, domains ...string) uuid.UUID {
	id := uuid.New()
	raw, _ := json.Marshal(rt)
	f.inv.Webservers = append(f.inv.Webservers, model.Webserver{Base: model.Base{ID: id}, HostID: hostID, Type: typ, Routing: raw})
	for _, d := range domains {
		f.listen(hostID, model.EndpointOwnerWebserver, id, model.EndpointKindDomain, d, 0)
	}
	return id
}

func (f *fixture) app(hostID uuid.UUID, vhosts ...string) uuid.UUID {
	id := uuid.New()
	f.inv.Apps = append(f.inv.Apps, model.App{Base: model.Base{ID: id}, HostID: hostID, Name: "app", Vhosts: vhosts})
	return id
}

func (f *fixture) dep(appID uuid.UUID, key, scheme string, port int, t *snapshot.Target) {
	d := model.AppDependency{AppID: appID, Key: key, Source: json.RawMessage(`{"file":".env","line":1}`)}
	if scheme != "" {
		d.Scheme = &scheme
	}
	if port != 0 {
		d.Port = &port
	}
	if t == nil {
		d.Unparsed = true
	} else {
		d.Target, _ = json.Marshal(t)
	}
	f.inv.Dependencies = append(f.inv.Dependencies, d)
}

// edge is a link as the tests compare it.
type edge struct {
	From, To   string
	Source     string
	Confidence string
	Rule       string
	Reason     string
	CDN        string
}

func (f *fixture) resolve(t *testing.T, cdn ...string) []edge {
	t.Helper()
	var opts Options
	if len(cdn) > 0 {
		custom := cdnpkg.CDN{Name: cdnpkg.Custom}
		for _, c := range cdn {
			custom.Ranges = append(custom.Ranges, netip.MustParsePrefix(c))
		}
		opts.CDN = append(opts.CDN, custom)
	}
	return f.resolveWith(t, opts)
}

func (f *fixture) resolveWith(t *testing.T, opts Options) []edge {
	t.Helper()
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	var out []edge
	for _, l := range Resolve(f.inv, opts, now) {
		assert.Equal(t, f.inv.Namespace.ID, l.NamespaceID)
		assert.Equal(t, now, l.ComputedAt)
		var d Detail
		require.NoError(t, json.Unmarshal(l.Detail, &d))
		e := edge{From: f.name(l.FromType, l.FromID), Source: l.Source, Confidence: l.Confidence, Rule: d.Rule, Reason: d.Reason, CDN: d.CDN}
		if l.ExternalTarget != nil {
			e.To = "external " + *l.ExternalTarget
		} else {
			e.To = f.name(l.ToType, *l.ToID)
		}
		out = append(out, e)
	}
	return out
}

// name names a node by its type and the hostname of its host.
func (f *fixture) name(typ string, id uuid.UUID) string {
	hostname := func(hostID uuid.UUID) string {
		for _, h := range f.inv.Hosts {
			if h.ID == hostID {
				return h.Hostname
			}
		}
		return "?"
	}
	switch typ {
	case model.LinkNodeHost:
		return "host " + hostname(id)
	case model.LinkNodeApp:
		for _, a := range f.inv.Apps {
			if a.ID == id {
				return "app " + hostname(a.HostID)
			}
		}
	case model.LinkNodeService:
		for _, s := range f.inv.Services {
			if s.ID == id {
				return s.Type + " " + hostname(s.HostID)
			}
		}
	case model.LinkNodeWebserver:
		for _, w := range f.inv.Webservers {
			if w.ID == id {
				return w.Type + " " + hostname(w.HostID)
			}
		}
	}
	return "?"
}

func ip(v string) *snapshot.Target   { return &snapshot.Target{Kind: "ip", Value: v} }
func sock(v string) *snapshot.Target { return &snapshot.Target{Kind: "socket", Value: v} }
func name(v, resolver string, resolved ...string) *snapshot.Target {
	return &snapshot.Target{Kind: "name", Value: v, Resolved: resolved, Resolver: snapshot.Resolver(resolver)}
}

const (
	env   = model.LinkSourceEnv
	exact = model.LinkConfidenceExact
)

func TestResolve_Dependencies(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
		port   int
		target *snapshot.Target
		want   []edge
	}{
		{name: "socket of the host", target: sock("/run/redis.sock"),
			want: []edge{{From: "app web", To: "redis web", Source: env, Confidence: exact, Rule: RuleSocket}}},
		{name: "socket nobody listens on", target: sock("/run/other.sock"),
			want: []edge{{From: "app web", To: "external web:unix:/run/other.sock", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonSocketNotFound}}},
		{name: "published docker port on loopback", port: 6380, target: ip("127.0.0.1"),
			want: []edge{{From: "app web", To: "redis web", Source: env, Confidence: exact, Rule: RuleLoopback}}},
		{name: "localhost port nobody listens on", port: 7000, target: name("localhost", "hosts", "127.0.0.1"),
			want: []edge{{From: "app web", To: "external web:127.0.0.1:7000", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonPortNotFound}}},
		{name: "private IP, port from the scheme", scheme: "postgresql", target: ip("10.0.1.50"),
			want: []edge{{From: "app web", To: "postgresql db", Source: env, Confidence: exact, Rule: RulePrivateIP}}},
		{name: "private IP held by two hosts: ambiguous", port: 6379, target: ip("10.0.1.61"),
			want: []edge{
				{From: "app web", To: "redis cache", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
				{From: "app web", To: "redis twin", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
			}},
		{name: "private IP of a host outside the networks of the app", port: 6379, target: ip("10.2.0.5"),
			want: []edge{{From: "app web", To: "redis twin", Source: env, Confidence: exact, Rule: RulePrivateIP}}},
		{name: "unknown private IP", port: 25, target: ip("10.0.1.90"),
			want: []edge{{From: "app web", To: "external 10.0.1.90", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonUnknownPrivate}}},
		{name: "hostname", port: 9200, target: name("DB", "hosts", "10.9.9.9"),
			want: []edge{{From: "app web", To: "elasticsearch db", Source: env, Confidence: exact, Rule: RuleHostname}}},
		{name: "route: the proxy the name resolves to, not the backend", port: 443, target: name("api.acme.example", "dns", "203.0.113.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: RuleRoute}}},
		{name: "route behind a CDN: the webserver declaring the name", port: 443, target: name("www.acme.example", "dns", "104.16.1.1"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: RuleRoute, CDN: cdnpkg.Custom}}},
		{name: "route resolved outside the inventory: inferred", port: 443, target: name("www.acme.example", "dns", "198.51.100.7"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: model.LinkConfidenceInferred, Rule: RuleRoute}}},
		{name: "route served by two webservers, resolved elsewhere: ambiguous", port: 443, target: name("api.acme.example", "failed"),
			want: []edge{
				{From: "app web", To: "traefik rp", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute},
				{From: "app web", To: "nginx web", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute},
			}},
		{name: "regex route", port: 443, target: name("eu.shop.acme.example", "dns", "203.0.113.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: RuleRoute}}},
		{name: "public IP without endpoint: the webserver of the host", port: 443, target: name("status.acme.example", "dns", "203.0.113.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: model.LinkConfidenceInferred, Rule: RulePublicIP}}},
		{name: "public IP outside the inventory", port: 443, target: name("s3.amazonaws.com", "dns", "52.95.155.20", "52.95.156.8"),
			want: []edge{{From: "app web", To: "external s3.amazonaws.com", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonPublic}}},
		{name: "CDN", port: 443, target: name("media.acme.example", "dns", "104.16.2.2"),
			want: []edge{{From: "app web", To: "external media.acme.example", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonCDN, CDN: cdnpkg.Custom}}},
		{name: "failed DNS", port: 443, target: name("api.partner.example", "failed"),
			want: []edge{{From: "app web", To: "external api.partner.example", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonDNSFailed}}},
		{name: "unparsed",
			want: []edge{{From: "app web", To: "external web:KEY", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonUnparsed}}},
		{name: "IP of a host without listener on the port", port: 8080, target: ip("10.0.1.50"),
			want: []edge{{From: "app web", To: "host db", Source: env, Confidence: model.LinkConfidenceInferred, Rule: RulePrivateIP}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.network("10.0.1.0/24")
			f.network("10.2.0.0/16")
			f.network("203.0.113.0/27")
			web := f.host("web", "10.0.1.21")
			f.service(web, "redis", model.EndpointKindUnix, "/run/redis.sock", 0)
			f.listen(web, model.EndpointOwnerService, f.inv.Services[0].ID, model.EndpointKindTCP, "127.0.0.1", 6380)
			f.webserver(web, "nginx", routing{}, "api.acme.example")
			db := f.host("db", "10.0.1.50")
			f.service(db, "postgresql", model.EndpointKindTCP, "0.0.0.0", 5432)
			f.service(db, "elasticsearch", model.EndpointKindTCP, "10.0.1.50", 9200)
			cache := f.host("cache", "10.0.1.61")
			f.service(cache, "redis", model.EndpointKindTCP, "0.0.0.0", 6379)
			// A clone keeping the IP of cache: nothing tells them apart.
			twin := f.host("twin", "10.0.1.61", "10.2.0.5")
			f.service(twin, "redis", model.EndpointKindTCP, "0.0.0.0", 6379)
			rp := f.host("rp", "203.0.113.10", "10.0.1.10")
			f.webserver(rp, "traefik", routing{}, "api.acme.example", "www.acme.example", `~^[a-z]+\.shop\.acme\.example$`)
			app := f.app(web)
			f.dep(app, "KEY", tt.scheme, tt.port, tt.target)

			assert.ElementsMatch(t, tt.want, f.resolve(t, "104.16.0.0/13"))
		})
	}
}

func TestResolve_NamedCDN(t *testing.T) {
	cloudflare, _ := cdnpkg.Known("cloudflare")
	f := newFixture()
	f.network("203.0.113.0/27")
	web := f.host("web", "10.0.1.21")
	rp := f.host("rp", "203.0.113.10")
	f.webserver(rp, "traefik", routing{}, "www.acme.example")
	app := f.app(web)
	f.dep(app, "SITE_URL", "https", 443, name("www.acme.example", "dns", "104.16.1.1"))
	f.dep(app, "MEDIA_URL", "https", 443, name("media.acme.example", "dns", "2606:4700::6810:84e5"))

	opts := Options{CDN: []cdnpkg.CDN{{Name: cdnpkg.Custom, Ranges: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}, cloudflare}}
	assert.ElementsMatch(t, []edge{
		{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: RuleRoute, CDN: "cloudflare"},
		{From: "app web", To: "external media.acme.example", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonCDN, CDN: "cloudflare"},
	}, f.resolveWith(t, opts))
}

func TestResolve_RouteBehindCDN(t *testing.T) {
	cloudflare, _ := cdnpkg.Known("cloudflare")
	f := newFixture()
	f.network("10.0.1.0/24")
	web := f.host("web", "10.0.1.21")
	f.webserver(web, "nginx", routing{Vhosts: []snapshot.Vhost{{ServerNames: []string{"cms.acme.example"}, ConfigFile: "cms.conf"}}}, "cms.acme.example")
	f.listen(web, model.EndpointOwnerWebserver, f.inv.Webservers[0].ID, model.EndpointKindTCP, "0.0.0.0", 80)
	rp := f.host("rp", "10.0.1.10")
	f.webserver(rp, "traefik", routing{
		Routers:  []snapshot.Router{{Name: "cms@file", Rule: "Host(`cms.acme.example`)", Service: "cms@file"}},
		Services: []snapshot.ProxyService{{Name: "cms@file", Servers: []string{"http://10.0.1.21"}}},
	}, "cms.acme.example")
	f.webserver(f.host("other", "10.0.1.30"), "nginx", routing{}, "cms.acme.example")
	api := f.host("api", "10.0.1.40")
	f.dep(f.app(api), "CMS_URL", "https", 443, name("cms.acme.example", "dns", "104.16.1.1"))

	got := f.resolveWith(t, Options{CDN: []cdnpkg.CDN{cloudflare}})
	var routed []edge
	for _, e := range got {
		if e.From == "app api" {
			routed = append(routed, e)
		}
	}
	assert.ElementsMatch(t, []edge{
		{From: "app api", To: "traefik rp", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute, CDN: "cloudflare"},
		{From: "app api", To: "nginx other", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute, CDN: "cloudflare"},
	}, routed, "the nginx the traefik routes the name to is behind it, not the nginx of another host")
}

func TestResolve_RouteReplicas(t *testing.T) {
	cloudflare, _ := cdnpkg.Known("cloudflare")
	tests := []struct {
		name   string
		target *snapshot.Target
		want   []edge
	}{
		{name: "behind a CDN: every replica, exact", target: name("cms.acme.example", "dns", "104.16.1.1"),
			want: []edge{
				{From: "app api", To: "traefik rp1", Source: env, Confidence: exact, Rule: RuleRoute, CDN: "cloudflare"},
				{From: "app api", To: "traefik rp2", Source: env, Confidence: exact, Rule: RuleRoute, CDN: "cloudflare"},
			}},
		{name: "resolved to one replica: the group", target: name("cms.acme.example", "dns", "10.0.1.11"),
			want: []edge{
				{From: "app api", To: "traefik rp1", Source: env, Confidence: exact, Rule: RuleRoute},
				{From: "app api", To: "traefik rp2", Source: env, Confidence: exact, Rule: RuleRoute},
			}},
		{name: "routed elsewhere by a leftover: ambiguous", target: name("www.acme.example", "dns", "104.16.1.1"),
			want: []edge{
				{From: "app api", To: "traefik rp1", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute, CDN: "cloudflare"},
				{From: "app api", To: "traefik rp2", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute, CDN: "cloudflare"},
				{From: "app api", To: "nginx old", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RuleRoute, CDN: "cloudflare"},
			}},
		{name: "resolved to the leftover: only it", target: name("www.acme.example", "dns", "10.0.1.30"),
			want: []edge{{From: "app api", To: "nginx old", Source: env, Confidence: exact, Rule: RuleRoute}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.network("10.0.1.0/24")
			cms := f.host("cms", "10.0.1.21")
			f.service(cms, "php-fpm", model.EndpointKindTCP, "0.0.0.0", 9000)
			site := f.host("site", "10.0.1.22")
			f.service(site, "node", model.EndpointKindTCP, "0.0.0.0", 3000)
			for i, ip := range []string{"10.0.1.11", "10.0.1.12"} {
				// The same routes on each replica.
				f.webserver(f.host(fmt.Sprintf("rp%d", i+1), ip), "traefik", routing{
					Routers: []snapshot.Router{
						{Name: "cms@file", Rule: "Host(`cms.acme.example`)", Service: "cms@file"},
						{Name: "www@file", Rule: "Host(`www.acme.example`)", Service: "www@file"},
					},
					Services: []snapshot.ProxyService{
						{Name: "cms@file", Servers: []string{"http://10.0.1.21:9000"}},
						{Name: "www@file", Servers: []string{"http://10.0.1.22:3000"}},
					},
				}, "cms.acme.example", "www.acme.example")
			}
			old := f.host("old", "10.0.1.30")
			f.webserver(old, "nginx", routing{}, "www.acme.example")
			api := f.host("api", "10.0.1.40")
			f.dep(f.app(api), "URL", "https", 443, tt.target)

			var got []edge
			for _, e := range f.resolveWith(t, Options{CDN: []cdnpkg.CDN{cloudflare}}) {
				if e.From == "app api" {
					got = append(got, e)
				}
			}
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestResolve_RoutePath(t *testing.T) {
	cloudflare, _ := cdnpkg.Known("cloudflare")
	tests := []struct {
		name, path string
		want       string
	}{
		{name: "a path both route to the site: replicas", path: "/contact", want: exact},
		{name: "a path only one routes to the tool: ambiguous", path: "/tool/x", want: model.LinkConfidenceAmbiguous},
		{name: "no path: every route, ambiguous", path: "", want: model.LinkConfidenceAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.network("10.0.1.0/24")
			f.service(f.host("site", "10.0.1.22"), "node", model.EndpointKindTCP, "0.0.0.0", 3000)
			f.service(f.host("tool", "10.0.1.23"), "node", model.EndpointKindTCP, "0.0.0.0", 4000)
			site := snapshot.Router{Name: "site@file", Rule: "Host(`www.acme.example`)", Service: "site@file"}
			tool := snapshot.Router{Name: "tool@file", Rule: "Host(`www.acme.example`) && PathPrefix(`/tool`)", Service: "tool@file"}
			services := []snapshot.ProxyService{{Name: "site@file", Servers: []string{"http://10.0.1.22:3000"}}, {Name: "tool@file", Servers: []string{"http://10.0.1.23:4000"}}}
			// rp2 lacks the route of the tool.
			f.webserver(f.host("rp1", "10.0.1.11"), "traefik", routing{Routers: []snapshot.Router{site, tool}, Services: services}, "www.acme.example")
			f.webserver(f.host("rp2", "10.0.1.12"), "traefik", routing{Routers: []snapshot.Router{site}, Services: services}, "www.acme.example")
			f.dep(f.app(f.host("api", "10.0.1.40")), "URL", "https", 443, name("www.acme.example", "dns", "104.16.1.1"))
			f.inv.Dependencies[0].URLPath = &tt.path

			var got []edge
			for _, e := range f.resolveWith(t, Options{CDN: []cdnpkg.CDN{cloudflare}}) {
				if e.From == "app api" {
					got = append(got, e)
				}
			}
			assert.ElementsMatch(t, []edge{
				{From: "app api", To: "traefik rp1", Source: env, Confidence: tt.want, Rule: RuleRoute, CDN: "cloudflare"},
				{From: "app api", To: "traefik rp2", Source: env, Confidence: tt.want, Rule: RuleRoute, CDN: "cloudflare"},
			}, got)
		})
	}
}

func TestResolve_VhostLocations(t *testing.T) {
	f := newFixture()
	f.network("10.0.1.0/24")
	web := f.host("web", "10.0.1.21")
	f.service(web, "php-fpm", model.EndpointKindTCP, "127.0.0.1", 9000)
	f.service(f.host("api", "10.0.1.22"), "node", model.EndpointKindTCP, "0.0.0.0", 3000)
	vhost := snapshot.Vhost{ServerNames: []string{"www.acme.example"}, ConfigFile: "site.conf", Locations: []snapshot.Location{
		{Upstreams: []string{"127.0.0.1:9000"}},
		{Path: "/api/", Upstreams: []string{"http://10.0.1.22:3000"}},
	}}
	f.webserver(web, "nginx", routing{Vhosts: []snapshot.Vhost{vhost}}, "www.acme.example")
	// An older agent sends no locations: any path.
	old := snapshot.Vhost{ServerNames: []string{"old.acme.example"}, ConfigFile: "old.conf", Upstreams: []string{"http://10.0.1.22:3000"}}
	f.webserver(f.host("old", "10.0.1.30"), "nginx", routing{Vhosts: []snapshot.Vhost{old}}, "old.acme.example")

	paths := map[string][]string{}
	for _, l := range Resolve(f.inv, Options{}, time.Now()) {
		var d Detail
		require.NoError(t, json.Unmarshal(l.Detail, &d))
		paths[f.name(l.FromType, l.FromID)+" > "+f.name(l.ToType, *l.ToID)] = pathmatch.For(d.Matches, nil)
	}
	assert.Equal(t, map[string][]string{
		"nginx web > php-fpm web": nil,
		"nginx web > node api":    {"/api/"},
		"nginx old > node api":    nil,
	}, paths)
}

func TestResolve_LocationOfVhostApp(t *testing.T) {
	f := newFixture()
	f.network("10.0.1.0/24")
	web := f.host("web", "10.0.1.21")
	site := f.app(web, "www.acme.example")
	simu := f.app(web, "www.acme.example")
	f.listen(web, model.EndpointOwnerApp, site, model.EndpointKindTCP, "127.0.0.1", 3000)
	f.listen(web, model.EndpointOwnerApp, simu, model.EndpointKindTCP, "127.0.0.1", 3103)
	vhost := snapshot.Vhost{ServerNames: []string{"www.acme.example"}, ConfigFile: "site.conf", Locations: []snapshot.Location{
		{Upstreams: []string{"http://127.0.0.1:3000"}},
		{Path: "/simu", Upstreams: []string{"http://127.0.0.1:3103"}},
	}}
	ws := f.webserver(web, "nginx", routing{Vhosts: []snapshot.Vhost{vhost}}, "www.acme.example")

	paths := map[uuid.UUID][]string{}
	for _, l := range Resolve(f.inv, Options{}, time.Now()) {
		if l.FromID == ws {
			var d Detail
			require.NoError(t, json.Unmarshal(l.Detail, &d))
			paths[*l.ToID] = pathmatch.For(d.Matches, nil)
		}
	}
	assert.Equal(t, map[uuid.UUID][]string{site: nil, simu: {"/simu"}}, paths,
		"both apps declare the name, the location passes /simu to one")
}

func TestPathsOfRule(t *testing.T) {
	tests := []struct {
		rule string
		want []string
	}{
		{"Host(`www.acme.example`)", nil},
		{"(Host(`www.acme.example`) || Host(`acme.example`)) && PathPrefix(`/inspirations-ia`)", []string{"/inspirations-ia"}},
		{"Host(`api.acme.example`) && PathRegexp(`^(/graphql|/api|/webhooks)`)", []string{"~^(/graphql|/api|/webhooks)"}},
		{"Host(`acme.example`) && Path(`/health`)", []string{"=/health"}},
		{"Host(`acme.example`) && PathPrefix(`/a`, `/b`)", []string{"/a", "/b"}},
		{"Host(`acme.example`) && !PathPrefix(`/admin`)", nil},
		{"Host(`acme.example`) && Path(`/items/{id:[0-9]+}`)", []string{"~^/items/([0-9]+)$"}},
		{"Host(`acme.example`) && PathPrefix(`/u/{name}`)", []string{"~^/u/[^/]+"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, pathsOfRule(tt.rule), tt.rule)
	}
}

func TestResolve_RouteDeployments(t *testing.T) {
	cloudflare, _ := cdnpkg.Known("cloudflare")
	f := newFixture()
	f.network("10.0.1.0/24")
	project, environment := uuid.New(), uuid.New()
	for _, h := range []struct{ name, ip, app string }{{"web1", "10.0.1.21", "website"}, {"web2", "10.0.1.22", "website"}, {"web3", "10.0.1.23", "other"}} {
		host := f.host(h.name, h.ip)
		f.webserver(host, "nginx", routing{Vhosts: []snapshot.Vhost{{ServerNames: []string{"www.acme.example"}}}}, "www.acme.example")
		f.app(host, "www.acme.example")
		a := &f.inv.Apps[len(f.inv.Apps)-1]
		a.Name, a.ProjectID, a.EnvironmentID = h.app, &project, &environment
	}
	api := f.host("api", "10.0.1.40")
	f.dep(f.app(api), "URL", "https", 443, name("www.acme.example", "dns", "10.0.1.21"))

	var got []edge
	for _, e := range f.resolveWith(t, Options{CDN: []cdnpkg.CDN{cloudflare}}) {
		if e.From == "app api" {
			got = append(got, e)
		}
	}
	assert.ElementsMatch(t, []edge{
		{From: "app api", To: "nginx web1", Source: env, Confidence: exact, Rule: RuleRoute},
		{From: "app api", To: "nginx web2", Source: env, Confidence: exact, Rule: RuleRoute},
	}, got, "the nginx serving another deployment of the app is a replica, not the one serving another app")
}

func TestResolve_Webservers(t *testing.T) {
	f := newFixture()
	f.network("10.0.1.0/24")
	web := f.host("web", "10.0.1.21")
	f.service(web, "php-fpm", model.EndpointKindUnix, "/run/php/app.sock", 0)
	f.webserver(web, "nginx", routing{
		Vhosts: []snapshot.Vhost{
			{ServerNames: []string{"app.example"}, Upstreams: []string{"backend"}, ConfigFile: "app.conf"},
			{ServerNames: []string{"front.example"}, Upstreams: []string{"http://127.0.0.1:3000"}, ConfigFile: "front.conf"},
		},
		Upstreams: []snapshot.Upstream{{Name: "backend", Servers: []string{"unix:/run/php/app.sock"}}},
	}, "app.example", "front.example")
	f.listen(web, model.EndpointOwnerWebserver, f.inv.Webservers[0].ID, model.EndpointKindTCP, "0.0.0.0", 80)
	f.app(web, "APP.example")
	f.app(web, "front.example")
	f.app(web, "unknown.example")

	db1, db2 := f.host("db1", "10.0.1.41"), f.host("db2", "10.0.1.42")
	f.service(db1, "mariadb", model.EndpointKindTCP, "0.0.0.0", 3306)
	f.service(db2, "mariadb", model.EndpointKindTCP, "0.0.0.0", 3306)
	lb := f.host("lb", "10.0.1.11")
	f.webserver(lb, "haproxy", routing{
		Frontends: []snapshot.Frontend{{Name: "db", DefaultBackend: "mariadb", Backends: []string{"mariadb"}}},
		Backends: []snapshot.Backend{{Name: "mariadb", Servers: []snapshot.BackendServer{
			{Name: "1", Address: "10.0.1.41:3306"}, {Name: "2", Address: "10.0.1.42:3306"}, {Name: "3", Address: "10.0.1.43:3306"}}}},
	})

	rp := f.host("rp", "10.0.1.10")
	f.webserver(rp, "traefik", routing{
		Routers: []snapshot.Router{
			{Name: "app-http@file", Rule: "Host(`app.example`)", Service: "noop@internal"},
			{Name: "app@file", Rule: "Host(`app.example`)", Service: "app@file"},
			{Name: "front@docker", Rule: "Host(`front.example`)", Service: "front"},
		},
		Services: []snapshot.ProxyService{
			{Name: "noop@internal"},
			{Name: "app@file", Servers: []string{"http://10.0.1.21"}},
			{Name: "front@docker", Servers: []string{"blue@docker", "green"}},
			{Name: "blue@docker", Servers: []string{"http://10.0.1.21:80"}},
			{Name: "green@docker", Servers: []string{"blue", "http://10.0.1.99:8080"}},
		},
	}, "app.example", "front.example")

	got := f.resolve(t)
	web1, wsrc, tsrc := "nginx web", model.LinkSourceWebserver, model.LinkSourceTraefik
	assert.ElementsMatch(t, []edge{
		{From: web1, To: "app web", Source: wsrc, Confidence: exact, Rule: RuleVhost},
		{From: web1, To: "app web", Source: wsrc, Confidence: exact, Rule: RuleVhost},
		{From: web1, To: "php-fpm web", Source: wsrc, Confidence: exact, Rule: RuleSocket},
		{From: "haproxy lb", To: "mariadb db1", Source: wsrc, Confidence: exact, Rule: RulePrivateIP},
		{From: "haproxy lb", To: "mariadb db2", Source: wsrc, Confidence: exact, Rule: RulePrivateIP},
		{From: "haproxy lb", To: "external 10.0.1.43", Source: wsrc, Confidence: exact, Rule: RuleExternal, Reason: ReasonUnknownPrivate},
		{From: "traefik rp", To: web1, Source: tsrc, Confidence: exact, Rule: RulePrivateIP},
		{From: "traefik rp", To: "external 10.0.1.99", Source: tsrc, Confidence: exact, Rule: RuleExternal, Reason: ReasonUnknownPrivate},
	}, got, "the front on :3000 nobody listens on is left out: the vhost ties it")

	var traefik Detail
	for _, l := range Resolve(f.inv, Options{}, time.Now()) {
		if l.Source == tsrc && l.ExternalTarget == nil {
			require.NoError(t, json.Unmarshal(l.Detail, &traefik))
		}
	}
	assert.Equal(t, []string{"app@file", "front@docker"}, traefik.Routes, "the routers to one webserver merge")
	assert.Equal(t, []string{"app.example", "front.example"}, traefik.Domains)
}

func TestParseServer(t *testing.T) {
	tests := []struct {
		in     string
		want   server
		parsed bool
	}{
		{"unix:/run/php/a.sock", server{Target: snapshot.Target{Kind: "socket", Value: "/run/php/a.sock"}}, true},
		{"unix:/run/php/a.sock:", server{Target: snapshot.Target{Kind: "socket", Value: "/run/php/a.sock"}}, true},
		{"http://10.0.1.21", server{Target: snapshot.Target{Kind: "ip", Value: "10.0.1.21"}, Port: 80, Scheme: "http"}, true},
		{"HTTPS://api.example:8443/v1", server{Target: snapshot.Target{Kind: "name", Value: "api.example"}, Port: 8443, Scheme: "https"}, true},
		{"127.0.0.1:9000", server{Target: snapshot.Target{Kind: "ip", Value: "127.0.0.1"}, Port: 9000}, true},
		{"[::1]:9000", server{Target: snapshot.Target{Kind: "ip", Value: "::1"}, Port: 9000}, true},
		{"http://[::1]", server{Target: snapshot.Target{Kind: "ip", Value: "::1"}, Port: 80, Scheme: "http"}, true},
		{"[fd00::5]", server{Target: snapshot.Target{Kind: "ip", Value: "fd00::5"}}, true},
		{"[fd00::5", server{}, false},
		{"10.0.0.1", server{Target: snapshot.Target{Kind: "ip", Value: "10.0.0.1"}}, true},
		{"backend", server{Target: snapshot.Target{Kind: "name", Value: "backend"}}, true},
		{"host:http", server{}, false},
		{"", server{}, false},
	}
	for _, tt := range tests {
		got, ok := parseServer(tt.in)
		assert.Equal(t, tt.parsed, ok, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestSameService(t *testing.T) {
	assert.True(t, sameService("a@file", "a@file"))
	assert.True(t, sameService("a@file", "a"))
	assert.True(t, sameService("a", "a@docker"))
	assert.False(t, sameService("a@file", "a@docker"))
	assert.False(t, sameService("a", "b"))
}

func TestListenPorts(t *testing.T) {
	assert.Equal(t, []int{80, 443, 8080, 3306}, listenPorts([]string{"80", "443 ssl http2", "[::]:443 ssl", "10.0.0.1:8080", "0.0.0.0:3306", "unix:/run/a.sock", "*:80"}))
	assert.Empty(t, listenPorts(nil))
}

func TestDefaultPort(t *testing.T) {
	assert.Equal(t, 5432, DefaultPort("PostgreSQL"))
	assert.Equal(t, 443, DefaultPort("https"))
	assert.Zero(t, DefaultPort("gopher"))
}

func TestResolve_SplitNetwork(t *testing.T) {
	tests := []struct {
		name  string
		moved []string
		want  []edge
	}{
		{name: "not split: ambiguous",
			want: []edge{
				{From: "app web", To: "redis cache", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
				{From: "app web", To: "redis twin", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
			}},
		{name: "the twin split apart: the cache", moved: []string{"twin"},
			want: []edge{{From: "app web", To: "redis cache", Source: env, Confidence: exact, Rule: RulePrivateIP}}},
		{name: "the app split with the twin: the twin", moved: []string{"twin", "web"},
			want: []edge{{From: "app web", To: "redis twin", Source: env, Confidence: exact, Rule: RulePrivateIP}}},
		{name: "the app split alone: none in its network, every holder", moved: []string{"web"},
			want: []edge{
				{From: "app web", To: "redis cache", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
				{From: "app web", To: "redis twin", Source: env, Confidence: model.LinkConfidenceAmbiguous, Rule: RulePrivateIP},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			lan := f.network("10.0.1.0/24")
			split := uuid.New()
			hosts := map[string]uuid.UUID{"web": f.host("web", "10.0.1.21")}
			hosts["cache"] = f.host("cache", "10.0.1.61")
			f.service(hosts["cache"], "redis", model.EndpointKindTCP, "0.0.0.0", 6379)
			hosts["twin"] = f.host("twin", "10.0.1.61")
			f.service(hosts["twin"], "redis", model.EndpointKindTCP, "0.0.0.0", 6379)
			for _, h := range tt.moved {
				f.inv.HostNetworks = append(f.inv.HostNetworks, model.HostNetwork{HostID: hosts[h], FromNetworkID: lan, NetworkID: split})
			}
			f.dep(f.app(hosts["web"]), "KEY", "", 6379, ip("10.0.1.61"))

			assert.ElementsMatch(t, tt.want, f.resolve(t))
		})
	}
}

func TestNetworks(t *testing.T) {
	f := newFixture()
	lan, wide, split := f.network("10.0.1.0/24"), f.network("10.0.0.0/8"), uuid.New()
	moved, stays := uuid.New(), uuid.New()
	f.inv.HostNetworks = []model.HostNetwork{{HostID: moved, FromNetworkID: lan, NetworkID: split}}
	n := NewNetworks(f.inv)

	tests := []struct {
		host uuid.UUID
		ip   string
		want uuid.UUID
		ok   bool
	}{
		{host: stays, ip: "10.0.1.5", want: lan, ok: true},
		{host: moved, ip: "10.0.1.5", want: split, ok: true},
		{host: moved, ip: "10.9.0.1", want: wide, ok: true},
		{host: moved, ip: "192.168.1.1"},
	}
	for _, tt := range tests {
		got, ok := n.Of(tt.host, netip.MustParseAddr(tt.ip))
		assert.Equal(t, tt.ok, ok, tt.ip)
		assert.Equal(t, tt.want, got, tt.ip)
	}
}
