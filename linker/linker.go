// Package linker computes the links of a namespace from its inventory: what
// each app connects to, and where each webserver sends its traffic. It is a
// pure function of the inventory, recomputed whole for a namespace when one
// of its hosts changes, so that an app enrolled before its database gets
// its link once the database comes.
package linker

import (
	"cmp"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// The rules a link was found by, in the order they are tried.
const (
	// RuleSocket is a unix socket of the same host.
	RuleSocket = "socket"
	// RuleLoopback is a loopback address, a port of the same host:
	// published docker ports included.
	RuleLoopback = "loopback"
	// RuleRoute is a name a webserver routes: Traefik Host() or vhost
	// server name.
	RuleRoute = "route"
	// RuleHostname is the name of a host.
	RuleHostname = "hostname"
	// RulePrivateIP is a private IP of a host sharing a network.
	RulePrivateIP = "private_ip"
	// RulePublicIP is an IP of the public network.
	RulePublicIP = "public_ip"
	// RuleVhost ties a webserver to the app its vhost serves.
	RuleVhost = "vhost"
	// RuleExternal is a target outside the inventory; Reason says why.
	RuleExternal = "external"
)

// The reasons of an external target.
const (
	ReasonUnparsed       = "unparsed"
	ReasonSocketNotFound = "socket_not_found"
	ReasonPortNotFound   = "port_not_listened"
	ReasonDNSFailed      = "dns_failed"
	ReasonCDN            = "cdn"
	ReasonPublic         = "public_ip"
	ReasonUnknownPrivate = "unknown_private_ip"
)

// Detail is what links.detail holds: how the link was found, and its
// proof.
type Detail struct {
	Rule string `json:"rule"`
	// Reason tells why a target is external.
	Reason string `json:"reason,omitempty"`
	// Key and Proof are the dependency of an app, and where it was read.
	Key   string          `json:"key,omitempty"`
	Proof json.RawMessage `json:"proof,omitempty"`
	// Target is the address as written: name, IP or socket, and port.
	Target string `json:"target,omitempty"`
	Port   int    `json:"port,omitempty"`
	// Via is the IP a name resolved to, or the CDN it goes through.
	Via string `json:"via,omitempty"`
	// Routes are the routers, vhosts or frontends → backends sending the
	// traffic of a webserver; Domains the names they route.
	Routes  []string `json:"routes,omitempty"`
	Domains []string `json:"domains,omitempty"`
}

// Resolve computes the links of the inventory; cdn are the IP ranges of
// the CDNs. The links have no id yet.
func Resolve(inv *model.Inventory, cdn []netip.Prefix, now time.Time) []model.Link {
	r := newResolver(inv, cdn)
	r.dependencies()
	r.vhosts()
	r.proxies()
	out := make([]model.Link, 0, len(r.links))
	for _, l := range r.links {
		l.link.NamespaceID, l.link.ComputedAt = inv.Namespace.ID, now
		l.link.Detail, _ = json.Marshal(l.detail)
		out = append(out, l.link)
	}
	return out
}

// node is an end of a link.
type node struct {
	Type string
	ID   uuid.UUID
	// External is the target of an external node.
	External string
}

// target is where an address leads.
type target struct {
	node
	rule, confidence, reason, via string
}

type pending struct {
	link   model.Link
	detail Detail
}

type resolver struct {
	inv       *model.Inventory
	cdn       []netip.Prefix
	hosts     map[uuid.UUID]*model.Host
	hostNames map[string]*model.Host
	// ipHosts are the hosts holding an IP, ipNetworks the network of an IP
	// by its CIDR, hostNetworks the networks a host is in: by the network
	// of the CIDR, the one it is in, which differs for a host moved into a
	// network split from it.
	ipHosts      map[netip.Addr][]uuid.UUID
	ipNetworks   map[netip.Addr]uuid.UUID
	hostNetworks map[uuid.UUID]map[uuid.UUID]uuid.UUID
	endpoints    map[uuid.UUID][]model.Endpoint
	domains      []model.Endpoint
	webservers   map[uuid.UUID][]*model.Webserver
	// webserverHosts is the host of each webserver.
	webserverHosts map[uuid.UUID]uuid.UUID
	links          []*pending
	merged         map[string]*pending
}

func newResolver(inv *model.Inventory, cdn []netip.Prefix) *resolver {
	r := &resolver{inv: inv, cdn: cdn, hosts: map[uuid.UUID]*model.Host{}, hostNames: map[string]*model.Host{},
		ipHosts: map[netip.Addr][]uuid.UUID{}, ipNetworks: map[netip.Addr]uuid.UUID{},
		hostNetworks: map[uuid.UUID]map[uuid.UUID]uuid.UUID{}, endpoints: map[uuid.UUID][]model.Endpoint{},
		webservers: map[uuid.UUID][]*model.Webserver{}, webserverHosts: map[uuid.UUID]uuid.UUID{}, merged: map[string]*pending{}}
	for i := range inv.Hosts {
		h := &inv.Hosts[i]
		r.hosts[h.ID] = h
		r.hostNames[strings.ToLower(h.Hostname)] = h
	}
	networks := NewNetworks(inv)
	for _, ip := range inv.IPs {
		r.ipHosts[ip.IP.Addr] = append(r.ipHosts[ip.IP.Addr], ip.HostID)
		if byCIDR, ok := networks.ByCIDR(ip.IP.Addr); ok {
			r.ipNetworks[ip.IP.Addr] = byCIDR
			if r.hostNetworks[ip.HostID] == nil {
				r.hostNetworks[ip.HostID] = map[uuid.UUID]uuid.UUID{}
			}
			r.hostNetworks[ip.HostID][byCIDR], _ = networks.Of(ip.HostID, ip.IP.Addr)
		}
	}
	for _, e := range inv.Endpoints {
		if e.Kind == model.EndpointKindDomain {
			r.domains = append(r.domains, e)
		} else {
			r.endpoints[e.HostID] = append(r.endpoints[e.HostID], e)
		}
	}
	for i := range inv.Webservers {
		w := &inv.Webservers[i]
		r.webservers[w.HostID] = append(r.webservers[w.HostID], w)
		r.webserverHosts[w.ID] = w.HostID
	}
	return r
}

// add records a link; with a merge key, a link of the same ends and source
// gets its routes and domains instead.
func (r *resolver) add(from node, to target, source string, detail Detail, merge bool) {
	if from.Type == to.Type && from.ID == to.ID && to.Type != model.LinkNodeExternal {
		return // a webserver proxying to itself
	}
	detail.Rule, detail.Reason = to.rule, to.reason
	if detail.Via == "" {
		detail.Via = to.via
	}
	key := strings.Join([]string{from.Type, from.ID.String(), to.Type, to.ID.String(), to.External, source}, "\x00")
	if p, ok := r.merged[key]; merge && ok {
		p.detail.Routes = appendNew(p.detail.Routes, detail.Routes...)
		p.detail.Domains = appendNew(p.detail.Domains, detail.Domains...)
		return
	}
	link := model.Link{FromType: from.Type, FromID: from.ID, ToType: to.Type, Source: source, Confidence: to.confidence}
	if to.Type == model.LinkNodeExternal {
		external := to.External
		link.ExternalTarget = &external
	} else {
		id := to.ID
		link.ToID = &id
	}
	p := &pending{link: link, detail: detail}
	r.links = append(r.links, p)
	if merge {
		r.merged[key] = p
	}
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

// dependencies links each app to what its dependencies target.
func (r *resolver) dependencies() {
	apps := map[uuid.UUID]*model.App{}
	for i := range r.inv.Apps {
		apps[r.inv.Apps[i].ID] = &r.inv.Apps[i]
	}
	for _, d := range r.inv.Dependencies {
		app, ok := apps[d.AppID]
		if !ok {
			continue
		}
		from := node{Type: model.LinkNodeApp, ID: app.ID}
		detail := Detail{Key: d.Key, Proof: d.Source}
		var t snapshot.Target
		if d.Unparsed || len(d.Target) == 0 || json.Unmarshal(d.Target, &t) != nil || t.Value == "" {
			r.add(from, r.external(r.local(app.HostID, d.Key), ReasonUnparsed), model.LinkSourceEnv, detail, false)
			continue
		}
		port := deref(d.Port)
		if port == 0 {
			port = DefaultPort(deref(d.Scheme))
		}
		detail.Target, detail.Port = t.Value, port
		for _, to := range r.address(app.HostID, t, port) {
			r.add(from, to, model.LinkSourceEnv, detail, false)
		}
	}
}

// vhosts links each webserver to the apps its vhosts serve.
func (r *resolver) vhosts() {
	for _, app := range r.inv.Apps {
		for _, w := range r.webservers[app.HostID] {
			var served []string
			for _, name := range app.Vhosts {
				if r.serves(w.ID, strings.ToLower(name)) {
					served = append(served, strings.ToLower(name))
				}
			}
			if len(served) > 0 {
				to := target{node: node{Type: model.LinkNodeApp, ID: app.ID}, rule: RuleVhost, confidence: model.LinkConfidenceExact}
				r.add(node{Type: model.LinkNodeWebserver, ID: w.ID}, to, model.LinkSourceWebserver, Detail{Domains: served}, true)
			}
		}
	}
}

func (r *resolver) serves(webserverID uuid.UUID, name string) bool {
	for _, d := range r.domains {
		if d.OwnerID == webserverID && domainMatches(d.Address, name) {
			return true
		}
	}
	return false
}

// routing is the part of webservers.routing the links need.
type routing struct {
	Vhosts    []snapshot.Vhost        `json:"vhosts"`
	Upstreams []snapshot.Upstream     `json:"upstreams"`
	Routers   []snapshot.Router       `json:"routers"`
	Services  []snapshot.ProxyService `json:"services"`
	Frontends []snapshot.Frontend     `json:"frontends"`
	Backends  []snapshot.Backend      `json:"backends"`
}

// proxies links each webserver to what it proxies to: the upstreams of
// its vhosts, the services of its Traefik routers, the servers of its
// HAProxy backends.
func (r *resolver) proxies() {
	for i := range r.inv.Webservers {
		w := &r.inv.Webservers[i]
		var rt routing
		if json.Unmarshal(w.Routing, &rt) != nil {
			continue
		}
		from := node{Type: model.LinkNodeWebserver, ID: w.ID}
		source := model.LinkSourceWebserver
		if w.Type == "traefik" {
			source = model.LinkSourceTraefik
		}
		for _, v := range rt.Vhosts {
			route := v.ConfigFile
			for _, u := range v.Upstreams {
				r.proxy(w.HostID, from, source, rt.upstreamServers(u), Detail{Routes: []string{route}, Domains: v.ServerNames}, true)
			}
		}
		for _, router := range rt.Routers {
			servers := rt.serviceServers(router.Service, map[string]bool{})
			r.proxy(w.HostID, from, source, servers, Detail{Routes: []string{router.Name}, Domains: domainsOfRule(router.Rule)}, false)
		}
		for _, f := range rt.Frontends {
			for _, name := range appendNew(nil, append([]string{f.DefaultBackend}, f.Backends...)...) {
				for _, b := range rt.Backends {
					if b.Name != name {
						continue
					}
					var servers []string
					for _, s := range b.Servers {
						servers = append(servers, s.Address)
					}
					r.proxy(w.HostID, from, source, servers, Detail{Routes: []string{f.Name + " → " + b.Name}}, false)
				}
			}
		}
	}
}

// proxy links a webserver to the servers it sends traffic to. With local
// set, a server of the host nobody listens on is left out: the vhost
// already ties the webserver to its app.
func (r *resolver) proxy(hostID uuid.UUID, from node, source string, servers []string, detail Detail, local bool) {
	for _, s := range servers {
		t, port, ok := parseServer(s)
		if !ok {
			continue
		}
		d := detail
		d.Target, d.Port = t.Value, port
		for _, to := range r.address(hostID, t, port) {
			if local && to.reason == ReasonPortNotFound {
				continue
			}
			r.add(from, to, source, d, true)
		}
	}
}

// upstreamServers gives the servers of a vhost upstream: those of the
// nginx upstream block it names, else itself.
func (rt routing) upstreamServers(upstream string) []string {
	name := upstream
	if u, err := url.Parse(upstream); err == nil && u.Host != "" {
		name = u.Host
	}
	for _, u := range rt.Upstreams {
		if u.Name == name {
			return u.Servers
		}
	}
	return []string{upstream}
}

// serviceServers gives the servers of a Traefik service; a weighted or
// failover service lists the names of other services as servers.
func (rt routing) serviceServers(name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	var out []string
	for _, s := range rt.Services {
		if !sameService(s.Name, name) {
			continue
		}
		for _, server := range s.Servers {
			if rt.hasService(server) {
				out = append(out, rt.serviceServers(server, seen)...)
			} else {
				out = append(out, server)
			}
		}
	}
	return out
}

func (rt routing) hasService(name string) bool {
	for _, s := range rt.Services {
		if sameService(s.Name, name) {
			return true
		}
	}
	return false
}

// sameService compares Traefik service names, a name without provider
// matching any provider.
func sameService(a, b string) bool {
	if a == b {
		return true
	}
	an, _, aok := strings.Cut(a, "@")
	bn, _, bok := strings.Cut(b, "@")
	return an == bn && (!aok || !bok)
}

// parseServer reads a proxied server: "unix:/run/php/x.sock",
// "http://10.0.1.21", "127.0.0.1:9000", "10.0.1.41:3306".
func parseServer(s string) (snapshot.Target, int, bool) {
	if socket, ok := strings.CutPrefix(s, "unix:"); ok {
		return snapshot.Target{Kind: "socket", Value: strings.TrimSuffix(socket, ":")}, 0, true
	}
	scheme := ""
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		if u.Scheme == "unix" {
			return snapshot.Target{Kind: "socket", Value: u.Path}, 0, true
		}
		scheme, s = u.Scheme, u.Host
	}
	host, portStr, err := net.SplitHostPort(s)
	port := 0
	if err != nil {
		host, port = s, DefaultPort(scheme)
	} else if port, err = strconv.Atoi(portStr); err != nil {
		return snapshot.Target{}, 0, false
	}
	if host == "" {
		return snapshot.Target{}, 0, false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return snapshot.Target{Kind: "ip", Value: host}, port, true
	}
	return snapshot.Target{Kind: "name", Value: host}, port, true
}

// address resolves a target seen from a host, by the rules in order.
func (r *resolver) address(fromHost uuid.UUID, t snapshot.Target, port int) []target {
	switch t.Kind {
	case "socket":
		for _, e := range r.endpoints[fromHost] {
			if e.Kind == model.EndpointKindUnix && e.Address == t.Value {
				return []target{owner(e, RuleSocket, model.LinkConfidenceExact)}
			}
		}
		return []target{r.external(r.local(fromHost, "unix:"+t.Value), ReasonSocketNotFound)}
	case "ip":
		ip, err := netip.ParseAddr(t.Value)
		if err != nil {
			return []target{r.external(t.Value, ReasonUnknownPrivate)}
		}
		return r.ip(fromHost, ip.Unmap(), port)
	default:
		return r.name(fromHost, t, port)
	}
}

func (r *resolver) name(fromHost uuid.UUID, t snapshot.Target, port int) []target {
	name := strings.ToLower(strings.TrimSuffix(t.Value, "."))
	if name == "localhost" {
		return r.ip(fromHost, netip.AddrFrom4([4]byte{127, 0, 0, 1}), port)
	}
	var resolved []netip.Addr
	for _, s := range t.Resolved {
		if ip, err := netip.ParseAddr(s); err == nil {
			resolved = append(resolved, ip.Unmap())
		}
	}
	if out := r.route(name, resolved); len(out) > 0 {
		return out
	}
	if h, ok := r.hostNames[name]; ok {
		return r.onHost(h.ID, netip.Addr{}, port, RuleHostname, model.LinkConfidenceExact, name)
	}
	if len(resolved) == 0 {
		return []target{r.external(name, ReasonDNSFailed)}
	}
	var out []target
	for _, ip := range resolved {
		for _, to := range r.ip(fromHost, ip, port) {
			if to.Type == model.LinkNodeExternal {
				// The name, not its IPs, is the external target.
				to.External, to.via = name, cmp.Or(to.via, ip.String())
			} else if to.via == "" {
				to.via = ip.String()
			}
			out = appendTarget(out, to)
		}
	}
	return out
}

func appendTarget(list []target, t target) []target {
	for _, x := range list {
		if x.node == t.node {
			return list
		}
	}
	return append(list, t)
}

// route returns the webservers routing a name. When the name resolves to
// a host of the inventory, only its webservers are kept: the reverse proxy
// in front, not the backends behind that also know the name.
func (r *resolver) route(name string, resolved []netip.Addr) []target {
	var all []uuid.UUID
	for _, d := range r.domains {
		if domainMatches(d.Address, name) && !slices.Contains(all, d.OwnerID) {
			all = append(all, d.OwnerID)
		}
	}
	if len(all) == 0 {
		return nil
	}
	var reached []uuid.UUID
	for _, id := range all {
		for _, ip := range resolved {
			if slices.Contains(r.ipHosts[ip], r.webserverHosts[id]) && !slices.Contains(reached, id) {
				reached = append(reached, id)
			}
		}
	}
	candidates, confidence, via := reached, model.LinkConfidenceExact, ""
	if len(reached) == 0 {
		// Resolved elsewhere, such as a CDN in front, or not resolved.
		candidates, confidence = all, model.LinkConfidenceInferred
		for _, ip := range resolved {
			if r.inCDN(ip) {
				via = ReasonCDN
			}
		}
	}
	if len(candidates) > 1 {
		confidence = model.LinkConfidenceAmbiguous
	}
	out := make([]target, 0, len(candidates))
	for _, id := range candidates {
		out = append(out, target{node: node{Type: model.LinkNodeWebserver, ID: id}, rule: RuleRoute, confidence: confidence, via: via})
	}
	return out
}

// ip resolves an IP seen from a host.
func (r *resolver) ip(fromHost uuid.UUID, ip netip.Addr, port int) []target {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return r.onHost(fromHost, netip.Addr{}, port, RuleLoopback, model.LinkConfidenceExact, ip.String())
	}
	if r.inCDN(ip) {
		return []target{r.external(ip.String(), ReasonCDN)}
	}
	holders := r.ipHosts[ip]
	public := isPublic(ip)
	rule := RulePrivateIP
	if public {
		rule = RulePublicIP
	} else if shared := r.sharing(fromHost, ip, holders); len(shared) > 0 {
		holders = shared
	}
	switch len(holders) {
	case 0:
		if public {
			return []target{r.external(ip.String(), ReasonPublic)}
		}
		return []target{r.external(ip.String(), ReasonUnknownPrivate)}
	case 1:
		return r.onHost(holders[0], ip, port, rule, model.LinkConfidenceExact, ip.String())
	}
	var out []target
	for _, h := range holders {
		out = append(out, r.onHost(h, ip, port, rule, model.LinkConfidenceAmbiguous, ip.String())...)
	}
	return out
}

// sharing keeps the holders of a private IP whose network the host is in:
// the same private IP may exist in two networks, told apart by CIDR or
// split by hand.
func (r *resolver) sharing(fromHost uuid.UUID, ip netip.Addr, holders []uuid.UUID) []uuid.UUID {
	byCIDR, ok := r.ipNetworks[ip]
	network, in := r.hostNetworks[fromHost][byCIDR]
	if !ok || !in {
		return nil
	}
	var out []uuid.UUID
	for _, h := range holders {
		if n, in := r.hostNetworks[h][byCIDR]; in && n == network {
			out = append(out, h)
		}
	}
	return out
}

// onHost finds what listens on a port of a host: on ip, or on every
// address when ip is not valid. A port nobody listens on leads to the
// webserver of the host for HTTP, else to the host itself.
func (r *resolver) onHost(hostID uuid.UUID, ip netip.Addr, port int, rule, confidence, written string) []target {
	var out []target
	for _, e := range r.endpoints[hostID] {
		if e.Kind != model.EndpointKindTCP || deref(e.Port) != port || !listensOn(e.Address, ip) {
			continue
		}
		out = appendTarget(out, owner(e, rule, confidence))
	}
	if len(out) > 0 {
		return out
	}
	if rule == RuleLoopback {
		return []target{r.external(r.local(hostID, net.JoinHostPort(written, strconv.Itoa(port))), ReasonPortNotFound)}
	}
	if (port == 80 || port == 443) && len(r.webservers[hostID]) == 1 {
		return []target{{node: node{Type: model.LinkNodeWebserver, ID: r.webservers[hostID][0].ID}, rule: rule, confidence: model.LinkConfidenceInferred}}
	}
	return []target{{node: node{Type: model.LinkNodeHost, ID: hostID}, rule: rule, confidence: model.LinkConfidenceInferred}}
}

// listensOn tells an endpoint address reachable on ip: the same IP, a
// wildcard, or any address when ip is not valid.
func listensOn(address string, ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	return addr.IsUnspecified() || addr.Unmap() == ip
}

func owner(e model.Endpoint, rule, confidence string) target {
	return target{node: node{Type: e.OwnerType, ID: e.OwnerID}, rule: rule, confidence: confidence}
}

func (r *resolver) external(value, reason string) target {
	return target{node: node{Type: model.LinkNodeExternal, External: value}, rule: RuleExternal,
		confidence: model.LinkConfidenceExact, reason: reason}
}

// local names an external target that only means something on its host,
// such as a socket: two hosts with the same socket path are two targets.
func (r *resolver) local(hostID uuid.UUID, value string) string {
	if h, ok := r.hosts[hostID]; ok {
		return h.Hostname + ":" + value
	}
	return value
}

func (r *resolver) inCDN(ip netip.Addr) bool {
	for _, p := range r.cdn {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// isPublic tells an IP routed on the Internet.
func isPublic(ip netip.Addr) bool {
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

// domainMatches tells a name served by a domain endpoint: the same name, a
// "*.example.com" or ".example.com" wildcard, or a "~regex".
func domainMatches(domain, name string) bool {
	switch {
	case domain == name:
		return true
	case strings.HasPrefix(domain, "~"):
		re, err := regexp.Compile(strings.TrimPrefix(domain, "~"))
		return err == nil && re.MatchString(name)
	case strings.HasPrefix(domain, "*."):
		return strings.HasSuffix(name, domain[1:])
	case strings.HasPrefix(domain, "."):
		return name == domain[1:] || strings.HasSuffix(name, domain)
	}
	return false
}

var (
	ruleHost   = regexp.MustCompile(`\bHost(?:SNI)?\(([^)]*)\)`)
	backquoted = regexp.MustCompile("`([^`]*)`")
)

// domainsOfRule reads the names of the Host() of a Traefik rule.
func domainsOfRule(rule string) []string {
	var out []string
	for _, m := range ruleHost.FindAllStringSubmatch(rule, -1) {
		for _, q := range backquoted.FindAllStringSubmatch(m[1], -1) {
			if q[1] != "*" {
				out = appendNew(out, strings.ToLower(q[1]))
			}
		}
	}
	return out
}

// defaultPorts are the ports of the schemes a dependency may omit it for.
var defaultPorts = map[string]int{
	"http": 80, "https": 443, "ws": 80, "wss": 443,
	"postgres": 5432, "postgresql": 5432, "pgsql": 5432,
	"mysql": 3306, "mariadb": 3306, "mysqli": 3306, "pdo-mysql": 3306,
	"redis": 6379, "rediss": 6379, "memcached": 11211,
	"mongodb": 27017, "amqp": 5672, "amqps": 5671,
	"smtp": 25, "smtps": 465, "elasticsearch": 9200,
}

// DefaultPort is the port of a scheme, 0 when unknown.
func DefaultPort(scheme string) int {
	return defaultPorts[strings.ToLower(scheme)]
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
