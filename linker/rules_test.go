package linker

import (
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
)

// TestResolve_Rules covers the local IPs, the loopback, the HTTP schemes
// and ports, and the webserver taken for a port nobody listens on.
func TestResolve_Rules(t *testing.T) {
	const (
		inferred = model.LinkConfidenceInferred
		route    = RuleRoute
		private  = RulePrivateIP
	)
	tests := []struct {
		name      string
		scheme    string
		port      int
		target    *snapshot.Target
		httpPorts []int
		want      []edge
	}{
		{name: "IP of docker0 of the caller, every Docker host holding it", port: 5432, target: ip("172.17.0.1"),
			want: []edge{{From: "app web", To: "postgresql web", Source: env, Confidence: exact, Rule: private}}},
		{name: "IP of a bridge of another host", port: 5432, target: ip("172.18.0.1"),
			want: []edge{{From: "app web", To: "external 172.18.0.1", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonUnknownPrivate}}},
		{name: "own IP held by a clone too: the caller", port: 6380, target: ip("10.0.1.21"),
			want: []edge{{From: "app web", To: "host web", Source: env, Confidence: inferred, Rule: private}}},
		{name: "own IP", port: 3306, target: ip("10.0.1.21"),
			want: []edge{{From: "app web", To: "mysql web", Source: env, Confidence: exact, Rule: private}}},
		{name: "loopback does not reach a service bound to a private IP", port: 3306, target: ip("127.0.0.1"),
			want: []edge{{From: "app web", To: "external web:127.0.0.1:3306", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonPortNotFound}}},
		{name: "loopback reaches a service bound to it", port: 6379, target: ip("127.0.0.1"),
			want: []edge{{From: "app web", To: "redis web", Source: env, Confidence: exact, Rule: RuleLoopback}}},
		{name: "127.0.0.1 does not reach ::1", port: 11211, target: ip("127.0.0.1"),
			want: []edge{{From: "app web", To: "external web:127.0.0.1:11211", Source: env, Confidence: exact, Rule: RuleExternal, Reason: ReasonPortNotFound}}},
		{name: "localhost reaches ::1", port: 11211, target: name("localhost", "hosts", "127.0.0.1", "::1"),
			want: []edge{{From: "app web", To: "memcached web", Source: env, Confidence: exact, Rule: RuleLoopback}}},
		{name: "0.0.0.0 reaches any address", port: 3306, target: ip("0.0.0.0"),
			want: []edge{{From: "app web", To: "mysql web", Source: env, Confidence: exact, Rule: RuleLoopback}}},
		{name: "mysql scheme never routed", scheme: "mysql", target: name("db.internal", "dns", "10.0.1.50"),
			want: []edge{{From: "app web", To: "mysql db", Source: env, Confidence: exact, Rule: private}}},
		{name: "no scheme, port outside the HTTP ports: not routed", port: 3306, target: name("db.internal", "dns", "10.0.1.50"),
			want: []edge{{From: "app web", To: "mysql db", Source: env, Confidence: exact, Rule: private}}},
		{name: "https scheme routed", scheme: "https", target: name("db.internal", "dns", "10.0.1.50"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: inferred, Rule: route}}},
		{name: "no scheme, HTTP port: routed", port: 443, target: name("api.example", "dns", "10.0.1.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: route}}},
		{name: "no scheme nor port: routed", target: name("api.example", "dns", "10.0.1.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: route}}},
		{name: "configured HTTP port: routed", port: 8000, httpPorts: []int{80, 443, 8000}, target: name("api.example", "dns", "10.0.1.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: exact, Rule: route}}},
		{name: "port outside the default HTTP ports: the host", port: 8000, target: name("api.example", "dns", "10.0.1.10"),
			want: []edge{{From: "app web", To: "host rp", Source: env, Confidence: inferred, Rule: private}}},
		{name: "configured HTTP port nobody listens on: the webserver", port: 8000, httpPorts: []int{80, 443, 8000}, target: ip("10.0.1.10"),
			want: []edge{{From: "app web", To: "traefik rp", Source: env, Confidence: inferred, Rule: private}}},
		{name: "the webserver listening on the port", port: 80, target: ip("10.0.1.11"),
			want: []edge{{From: "app web", To: "nginx edge", Source: env, Confidence: exact, Rule: private}}},
		{name: "not the webserver listening elsewhere: the one without known ports", port: 443, target: ip("10.0.1.11"),
			want: []edge{{From: "app web", To: "traefik edge", Source: env, Confidence: inferred, Rule: private}}},
		{name: "two webservers without known ports: the host", port: 80, target: ip("10.0.1.12"),
			want: []edge{{From: "app web", To: "host duo", Source: env, Confidence: inferred, Rule: private}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.network("10.0.1.0/24")
			web := f.host("web", "10.0.1.21")
			f.iface(web, "docker0", "172.17.0.1")
			f.service(web, "mysql", model.EndpointKindTCP, "10.0.1.21", 3306)
			f.service(web, "redis", model.EndpointKindTCP, "127.0.0.1", 6379)
			f.service(web, "memcached", model.EndpointKindTCP, "::1", 11211)
			f.service(web, "postgresql", model.EndpointKindTCP, "172.17.0.1", 5432)
			other := f.host("other", "10.0.1.22")
			f.iface(other, "docker0", "172.17.0.1")
			f.iface(other, "br-0a1b2c", "172.18.0.1")
			f.service(other, "postgresql", model.EndpointKindTCP, "0.0.0.0", 5432)
			clone := f.host("clone", "10.0.1.21")
			f.service(clone, "redis", model.EndpointKindTCP, "0.0.0.0", 6380)
			db := f.host("db", "10.0.1.50")
			f.service(db, "mysql", model.EndpointKindTCP, "0.0.0.0", 3306)
			rp := f.host("rp", "10.0.1.10")
			f.webserver(rp, "traefik", routing{}, "*.internal", "api.example")
			edgeHost := f.host("edge", "10.0.1.11")
			nginx := f.webserver(edgeHost, "nginx", routing{})
			f.listen(edgeHost, model.EndpointOwnerWebserver, nginx, model.EndpointKindTCP, "0.0.0.0", 80)
			f.webserver(edgeHost, "traefik", routing{})
			duo := f.host("duo", "10.0.1.12")
			f.webserver(duo, "traefik", routing{})
			f.webserver(duo, "haproxy", routing{})
			f.dep(f.app(web), "KEY", tt.scheme, tt.port, tt.target)

			assert.ElementsMatch(t, tt.want, f.resolveWith(t, Options{HTTPPorts: tt.httpPorts}))
		})
	}
}

func TestResolve_NginxUpstreamDefaultPort(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		servers  []string
		want     []edge
	}{
		{name: "server without port: 80", upstream: "http://backend", servers: []string{"10.0.1.50"},
			want: []edge{{From: "nginx web", To: "gunicorn db", Source: model.LinkSourceWebserver, Confidence: exact, Rule: RulePrivateIP}}},
		{name: "server with its port", upstream: "http://backend", servers: []string{"10.0.1.50:8080"},
			want: []edge{{From: "nginx web", To: "host db", Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceInferred, Rule: RulePrivateIP}}},
		{name: "upstream scheme: a name the proxy routes", upstream: "https://backend", servers: []string{"api.example"},
			want: []edge{{From: "nginx web", To: "traefik rp", Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceInferred, Rule: RuleRoute}}},
		{name: "fastcgi upstream: not routed", upstream: "backend", servers: []string{"api.example:9000"},
			want: []edge{{From: "nginx web", To: "external api.example", Source: model.LinkSourceWebserver, Confidence: exact, Rule: RuleExternal, Reason: ReasonDNSFailed}}},
		{name: "bracketed IPv6 URL", upstream: "http://[fd00::50]", servers: nil,
			want: []edge{{From: "nginx web", To: "gunicorn db", Source: model.LinkSourceWebserver, Confidence: exact, Rule: RulePrivateIP}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.network("10.0.1.0/24")
			f.network("fd00::/64")
			web := f.host("web", "10.0.1.21")
			var upstreams []snapshot.Upstream
			if tt.servers != nil {
				upstreams = []snapshot.Upstream{{Name: "backend", Servers: tt.servers}}
			}
			f.webserver(web, "nginx", routing{
				Vhosts:    []snapshot.Vhost{{ServerNames: []string{"www.example"}, Upstreams: []string{tt.upstream}, ConfigFile: "www.conf"}},
				Upstreams: upstreams,
			})
			db := f.host("db", "10.0.1.50", "fd00::50")
			f.service(db, "gunicorn", model.EndpointKindTCP, "0.0.0.0", 80)
			f.listen(db, model.EndpointOwnerService, f.inv.Services[0].ID, model.EndpointKindTCP, "::", 80)
			rp := f.host("rp", "10.0.1.10")
			f.webserver(rp, "traefik", routing{}, "api.example")

			assert.ElementsMatch(t, tt.want, f.resolve(t))
		})
	}
}

func TestIsLocalInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"docker0", true},
		{"docker_gwbridge", true},
		{"br-0a1b2c3d4e5f", true},
		{"cni0", true},
		{"virbr0", true},
		{"eth0", false},
		{"ens18", false},
		{"bond0", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, IsLocalInterface(tt.name), tt.name)
	}
}
