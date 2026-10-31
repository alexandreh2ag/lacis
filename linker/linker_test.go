package linker

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
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
}

func (f *fixture) resolve(t *testing.T, cdn ...string) []edge {
	t.Helper()
	var prefixes []netip.Prefix
	for _, c := range cdn {
		prefixes = append(prefixes, netip.MustParsePrefix(c))
	}
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	var out []edge
	for _, l := range Resolve(f.inv, prefixes, now) {
		assert.Equal(t, f.inv.Namespace.ID, l.NamespaceID)
		assert.Equal(t, now, l.ComputedAt)
		var d Detail
		require.NoError(t, json.Unmarshal(l.Detail, &d))
		e := edge{From: f.name(l.FromType, l.FromID), Source: l.Source, Confidence: l.Confidence, Rule: d.Rule, Reason: d.Reason}
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
		{name: "route behind a CDN: inferred", port: 443, target: name("www.acme.example", "dns", "104.16.1.1"),
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
			want: []edge{{From: "app web", To: "external media.acme.example", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonCDN}}},
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
	for _, l := range Resolve(f.inv, nil, time.Now()) {
		if l.Source == tsrc && l.ExternalTarget == nil {
			require.NoError(t, json.Unmarshal(l.Detail, &traefik))
		}
	}
	assert.Equal(t, []string{"app@file", "front@docker"}, traefik.Routes, "the routers to one webserver merge")
	assert.Equal(t, []string{"app.example", "front.example"}, traefik.Domains)
}

func TestDomainMatches(t *testing.T) {
	tests := []struct {
		domain, name string
		want         bool
	}{
		{"a.example", "a.example", true},
		{"a.example", "b.example", false},
		{"*.example", "a.example", true},
		{"*.example", "example", false},
		{".example", "example", true},
		{".example", "a.b.example", true},
		{`~^api\.`, "api.example", true},
		{`~^api\.`, "www.example", false},
		{`~(`, "x", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, domainMatches(tt.domain, tt.name), "%s ~ %s", tt.domain, tt.name)
	}
}

func TestParseServer(t *testing.T) {
	tests := []struct {
		in     string
		want   snapshot.Target
		port   int
		parsed bool
	}{
		{"unix:/run/php/a.sock", snapshot.Target{Kind: "socket", Value: "/run/php/a.sock"}, 0, true},
		{"unix:/run/php/a.sock:", snapshot.Target{Kind: "socket", Value: "/run/php/a.sock"}, 0, true},
		{"http://10.0.1.21", snapshot.Target{Kind: "ip", Value: "10.0.1.21"}, 80, true},
		{"https://api.example:8443/v1", snapshot.Target{Kind: "name", Value: "api.example"}, 8443, true},
		{"127.0.0.1:9000", snapshot.Target{Kind: "ip", Value: "127.0.0.1"}, 9000, true},
		{"[::1]:9000", snapshot.Target{Kind: "ip", Value: "::1"}, 9000, true},
		{"backend", snapshot.Target{Kind: "name", Value: "backend"}, 0, true},
		{"host:http", snapshot.Target{}, 0, false},
		{"", snapshot.Target{}, 0, false},
	}
	for _, tt := range tests {
		got, port, ok := parseServer(tt.in)
		assert.Equal(t, tt.parsed, ok, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
		assert.Equal(t, tt.port, port, tt.in)
	}
}

func TestSameService(t *testing.T) {
	assert.True(t, sameService("a@file", "a@file"))
	assert.True(t, sameService("a@file", "a"))
	assert.True(t, sameService("a", "a@docker"))
	assert.False(t, sameService("a@file", "a@docker"))
	assert.False(t, sameService("a", "b"))
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
